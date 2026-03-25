// Copyright 2025 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package soong_api

import (
	"android/soong/android"
	"android/soong/cc"
	"android/soong/java"
	"android/soong/rust"
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	android.RegisterParallelSingletonType("soong_api_db", soongApiSingletonFactory)
}

var soongApiPctx = android.NewPackageContext("android/soong/android/soong_api")

// SoongApiModuleRecord represents a single entry in the Soong API database.
// The term "Record" is used to clarify that this is a data snapshot of a
// module's properties intended for database storage (soong_api.db),
// rather than a functional Soong module object.
type SoongApiModuleRecord struct {
	// Identity
	Name     string `json:"name"`
	Type     string `json:"type"`
	BaseType string `json:"base_type,omitempty"`

	// Location
	Path string `json:"path"`

	// Target / Variation Info
	Os            string `json:"os,omitempty"`
	Arch          string `json:"arch,omitempty"`
	IsPrimaryArch bool   `json:"is_primary_arch"`
	Variant       string `json:"variant,omitempty"`

	// Status
	Enabled *bool `json:"enabled,omitempty"`

	// Artifacts
	TrendyTeamId                     string   `json:"trendy_team_id,omitempty"`
	InstallFiles                     []string `json:"install_files,omitempty"`
	BuiltFiles                       []string `json:"built_files,omitempty"`
	Licenses                         []string `json:"license,omitempty"`
	LicenseKinds                     []string `json:"license_kinds,omitempty"`
	LicenseText                      []string `json:"license_text,omitempty"`
	LicensePackageName               string   `json:"license_package_name,omitempty"`
	PackageDefaultApplicableLicenses []string `json:"package_default_applicable_licenses,omitempty"` // module_type=package
	LicenseKindConditions            []string `json:"license_kind_conditions,omitempty"`
	LicenseKindUrl                   string   `json:"license_kind_url,omitempty"`

	// Test related
	TestOnly       bool `json:"test_only,omitempty"`
	TopLevelTarget bool `json:"top_level_target,omitempty"`

	// Java / CC / Rust
	Libs           []string `json:"libs,omitempty"`             // Java javalib / CC sharedLibs
	LibFiles       []string `json:"lib_files,omitempty"`        // Path to jars or .a
	StaticLibs     []string `json:"static_libs,omitempty"`      // Java staticlib / CC staticLibs
	StaticLibFiles []string `json:"static_lib_files,omitempty"` // Path to jars or .a

	// For CC / Rust
	WholeStaticLibs     []string `json:"whole_static_libs,omitempty"`
	WholeStaticLibFiles []string `json:"whole_static_lib_files,omitempty"`
	HeaderLibs          []string `json:"header_libs,omitempty"`

	// CRT
	CrtLibs     []string `json:"crt_libs,omitempty"`
	CrtLibFiles []string `json:"crt_lib_files,omitempty"`
}

func soongApiSingletonFactory() android.Singleton {
	return &soongApiSingleton{}
}

type soongApiSingleton struct{}

func (c *soongApiSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	var records []SoongApiModuleRecord

	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		commonInfo, ok := android.OtherModuleProvider(ctx, m, android.CommonModuleInfoProvider)

		if !ok {
			return
		}

		record := SoongApiModuleRecord{
			Name:          ctx.ModuleName(m),
			Type:          ctx.ModuleType(m),
			Path:          ctx.ModuleDir(m),
			Variant:       ctx.ModuleSubDir(m),
			IsPrimaryArch: ctx.IsPrimaryModule(m),
		}

		// Fill metadata by categories
		c.fillCommonMetadata(&record, commonInfo)
		c.fillLicenseMetadata(ctx, m, &record, commonInfo)
		c.fillTeamMetadata(ctx, m, &record)
		c.fillTestMetadata(&record, commonInfo)
		c.fillLanguageMetadata(ctx, m, &record)

		// Final data deduplication and cleanup
		c.cleanupRecord(&record)

		records = append(records, record)
	})

	// Export collected data to JSON, ZIP and DB
	c.exportRecords(ctx, records)
}

// fillCommonMetadata extracts basic platform and build artifact information.
func (c *soongApiSingleton) fillCommonMetadata(record *SoongApiModuleRecord, common *android.CommonModuleInfo) {
	if !common.Enabled {
		record.Enabled = proptools.BoolPtr(false)
	}
	if common.BaseModuleType != "" {
		record.BaseType = common.BaseModuleType
	}

	record.Os = common.Target.Os.Name
	record.Arch = common.Target.ArchVariation()

	if common.InstallFiles != nil {
		record.InstallFiles = pathsToStrings(common.InstallFiles.InstallFiles)
	}
	if common.OutputFiles != nil {
		record.BuiltFiles = pathsToStrings(common.OutputFiles.DefaultOutputFiles)
	}
}

// fillLicenseMetadata extracts licensing, package and team ownership information.
func (c *soongApiSingleton) fillLicenseMetadata(ctx android.SingletonContext, m android.ModuleProxy, record *SoongApiModuleRecord, common *android.CommonModuleInfo) {
	if licInfo, ok := android.OtherModuleProvider(ctx, m, android.LicenseInfoProvider); ok {
		record.LicensePackageName = proptools.String(licInfo.PackageName)
		record.LicenseKinds = licInfo.EffectiveLicenseKinds
		record.LicenseText = licInfo.EffectiveLicenseText.Strings()
	}

	if record.Type == "package" && common.PackageInfo != nil {
		record.PackageDefaultApplicableLicenses = common.PackageInfo.PrimaryLicenses
	}

	if common.Licenses != nil {
		record.Licenses = common.Licenses.Licenses
	}

	if kindInfo, ok := android.OtherModuleProvider(ctx, m, android.LicenseKindInfoProvider); ok {
		record.LicenseKindConditions = kindInfo.Conditions
		record.LicenseKindUrl = kindInfo.Url
	}
}

// fillTeamMetadata extracts team ownership information.
func (c *soongApiSingleton) fillTeamMetadata(ctx android.SingletonContext, m android.ModuleProxy, record *SoongApiModuleRecord) {
	if team, ok := android.OtherModuleProvider(ctx, m, android.TeamInfoProvider); ok {
		record.TrendyTeamId = proptools.String(team.Properties.Trendy_team_id)
	}
}

// fillTestMetadata extracts test-specific flags.
func (c *soongApiSingleton) fillTestMetadata(record *SoongApiModuleRecord, common *android.CommonModuleInfo) {
	if common.TestModuleInfo != nil {
		record.TestOnly = common.TestModuleInfo.TestOnly
		record.TopLevelTarget = common.TestModuleInfo.TopLevelTarget
	}
}

// fillLanguageMetadata handles Java, CC, and Rust specific logic including dependency analysis.
func (c *soongApiSingleton) fillLanguageMetadata(ctx android.SingletonContext, m android.ModuleProxy, record *SoongApiModuleRecord) {
	_, isJava := android.OtherModuleProvider(ctx, m, java.JavaInfoProvider)
	_, isCc := android.OtherModuleProvider(ctx, m, cc.CcInfoProvider)
	_, isRust := android.OtherModuleProvider(ctx, m, rust.RustInfoProvider)

	// If CC, enhance BuiltFiles with precise output path
	if isCc {
		if linkableInfo, ok := android.OtherModuleProvider(ctx, m, cc.LinkableInfoProvider); ok {
			if linkableInfo.OutputFile.Valid() {
				record.BuiltFiles = append(record.BuiltFiles, linkableInfo.OutputFile.Path().String())
			}
		}
	}

	// Optimization: Only visit dependencies if the module is a known compiled type
	if !isJava && !isCc && !isRust {
		return
	}

	// Single pass dependency visitor for all language types
	ctx.VisitDirectDepsProxies(m, func(dep android.ModuleProxy) {
		tag := ctx.OtherModuleDependencyTag(dep)
		depName := ctx.ModuleName(dep)

		if isJava {
			if depJavaInfo, ok := android.OtherModuleProvider(ctx, dep, java.JavaInfoProvider); ok {
				depFiles := depJavaInfo.ImplementationJars.Strings()
				if java.IsStaticLibDepTag(tag) {
					record.StaticLibs = append(record.StaticLibs, depName)
					record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
				} else if java.IsRuntimeDepTag(tag) {
					record.Libs = append(record.Libs, depName)
					record.LibFiles = append(record.LibFiles, depFiles...)
				}
			}
		}

		if isCc || isRust {
			var depFiles []string
			if depLinkable, ok := android.OtherModuleProvider(ctx, dep, cc.LinkableInfoProvider); ok {
				if depLinkable.OutputFile.Valid() {
					depFiles = append(depFiles, depLinkable.OutputFile.Path().String())
				}
			}

			if isCc {
				if cc.IsWholeStaticDepTag(tag) {
					record.WholeStaticLibs = append(record.WholeStaticLibs, depName)
					record.WholeStaticLibFiles = append(record.WholeStaticLibFiles, depFiles...)
				} else if cc.IsStaticDepTag(tag) {
					record.StaticLibs = append(record.StaticLibs, depName)
					record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
				} else if cc.IsCrtDepTag(tag) {
					record.CrtLibs = append(record.CrtLibs, depName)
					record.CrtLibFiles = append(record.CrtLibFiles, depFiles...)
				} else if cc.IsSharedDepTag(tag) {
					record.Libs = append(record.Libs, depName)
					record.LibFiles = append(record.LibFiles, depFiles...)
				} else if cc.IsHeaderDepTag(tag) {
					record.HeaderLibs = append(record.HeaderLibs, depName)
				}
			}

			if isRust {
				if rust.IsRlibDepTag(tag) || cc.IsStaticDepTag(tag) {
					record.StaticLibs = append(record.StaticLibs, depName)
					record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
				}
				if cc.IsWholeStaticDepTag(tag) {
					record.WholeStaticLibs = append(record.WholeStaticLibs, depName)
					record.WholeStaticLibFiles = append(record.WholeStaticLibFiles, depFiles...)
				}
			}
		}
	})
}

// cleanupRecord performs final deduplication of collected strings.
func (c *soongApiSingleton) cleanupRecord(record *SoongApiModuleRecord) {
	record.BuiltFiles = android.FirstUniqueStrings(record.BuiltFiles)
	record.StaticLibs = android.FirstUniqueStrings(record.StaticLibs)
	record.StaticLibFiles = android.FirstUniqueStrings(record.StaticLibFiles)
	record.Libs = android.FirstUniqueStrings(record.Libs)
	record.LibFiles = android.FirstUniqueStrings(record.LibFiles)
	record.WholeStaticLibs = android.FirstUniqueStrings(record.WholeStaticLibs)
	record.WholeStaticLibFiles = android.FirstUniqueStrings(record.WholeStaticLibFiles)
	record.HeaderLibs = android.FirstUniqueStrings(record.HeaderLibs)
}

// exportRecords serializes the metadata and sets up the build rules for the zip and database.
func (c *soongApiSingleton) exportRecords(ctx android.SingletonContext, records []SoongApiModuleRecord) {
	jsonData, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		ctx.Errorf("Failed to marshal soong api records: %s", err)
		return
	}

	// Create the ZIP content directly in memory.
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)

	// Create a file entry within the ZIP named "soong_api.json".
	f, err := zipWriter.Create("soong_api.json")
	if err != nil {
		ctx.Errorf("Failed to create zip entry: %s", err)
		return
	}
	if _, err := f.Write(jsonData); err != nil {
		ctx.Errorf("Failed to write json to zip: %s", err)
		return
	}
	zipWriter.Close()

	// Use TARGET_PRODUCT (DeviceProduct) to partition the output directory.
	product := "generic" // Fallback for safety
	if ctx.Config().HasDeviceProduct() {
		product = ctx.Config().DeviceProduct()
	}

	// Output path for soong_api.zip
	// Path: out/soong/soong_api/<product>/soong_api.zip
	zipPath := android.PathForOutput(ctx, "soong_api", product, "soong_api.zip")
	WriteContentToFile(zipPath, zipBuf.String())

	ctx.DistForGoal("droid", zipPath)

	// Generate the soong_api.db using the ZIP file as the input source.
	// Path: out/soong/soong_api/<product>/soong_api.db
	soongApiDbPath := android.PathForOutput(ctx, "soong_api", product, "soong_api.db")

	dbRb := android.NewRuleBuilder(soongApiPctx, ctx)

	loaderPath := ctx.Config().HostToolPath(ctx, "soong_api_db_loader")

	// Build the command: <loader> -i <zip_file_input> -o <db_output>
	dbRb.Command().
		Tool(loaderPath).
		FlagWithInput("-i ", zipPath).
		FlagWithOutput("-o ", soongApiDbPath)

	dbRb.Build("build_soong_api_db", "Building soong_api.db from soong_api.zip")

	// Phony target for 'm soong_api.db'
	ctx.Build(soongApiPctx, android.BuildParams{
		Rule:   blueprint.Phony,
		Input:  soongApiDbPath,
		Output: android.PathForPhony(ctx, "soong_api.db"),
	})
}

func pathsToStrings[T android.Path](paths []T) []string {
	if len(paths) == 0 {
		return nil
	}
	ret := make([]string, len(paths))
	for i, p := range paths {
		ret[i] = p.String()
	}
	return ret
}

// WriteContentToFile writes content to the given Path no matter what the file exist.
func WriteContentToFile(path android.Path, content string) {
	// 1. Convert Path to an absolute path string (e.g., "/usr/local/xxx/git_main/out/soong/soong_api/...")
	filePath := absolutePath(path.String())

	// 2. Get the directory path
	dir := filepath.Dir(filePath)

	// 3. Create the directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Errorf("failed to create directory %q: %w", dir, err))
	}

	// 4. Create the file
	f, err := os.Create(filePath)
	if err != nil {
		panic(fmt.Errorf("failed to create file %q: %w", filePath, err))
	}
	defer f.Close()

	// 5. Write content
	if _, err := io.WriteString(f, content); err != nil {
		panic(fmt.Errorf("failed to write content to %q: %w", filePath, err))
	}
}

func absolutePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(android.AbsSrcDirForExistingUseCases(), path)
}
