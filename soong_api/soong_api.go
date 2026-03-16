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
	Name string `json:"name"`
	Type string `json:"type"`

	// Location
	Path string `json:"path"`

	// Target / Variation Info
	Os            string `json:"os,omitempty"`
	Arch          string `json:"arch,omitempty"`
	IsPrimaryArch bool   `json:"is_primary_arch"`
	Variant       string `json:"variant,omitempty"`

	// Status
	Enabled bool `json:"enabled"`

	// Artifacts
	TrendyTeamId                     string   `json:"trendy_team_id,omitempty"`
	InstallFiles                     []string `json:"install_files,omitempty"`
	BuiltFiles                       []string `json:"built_files,omitempty"`
	Licenses                         []string `json:"license,omitempty"`
	PackageDefaultApplicableLicenses []string `json:"package_default_applicable_licenses,omitempty"` // module_type=package

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
			Enabled:       commonInfo.Enabled,
			Variant:       ctx.ModuleSubDir(m),
			IsPrimaryArch: ctx.IsPrimaryModule(m),
		}

		if record.Type == "package" && commonInfo.PackageInfo != nil {
			record.PackageDefaultApplicableLicenses = commonInfo.PackageInfo.PrimaryLicenses
		}

		// Extract OS / Arch
		record.Os = commonInfo.Target.Os.Name
		record.Arch = commonInfo.Target.Arch.ArchType.Name

		if team, ok := android.OtherModuleProvider(ctx, m, android.TeamInfoProvider); ok {
			record.TrendyTeamId = proptools.String(team.Properties.Trendy_team_id)
		}

		if commonInfo.InstallFiles != nil {
			record.InstallFiles = pathsToStrings(commonInfo.InstallFiles.InstallFiles)
		}

		if commonInfo.OutputFiles != nil {
			record.BuiltFiles = pathsToStrings(commonInfo.OutputFiles.DefaultOutputFiles)
		}

		if commonInfo.Licenses != nil {
			record.Licenses = commonInfo.Licenses.Licenses
		}

		if _, ok := android.OtherModuleProvider(ctx, m, java.JavaInfoProvider); ok {

			// Module name of Java libs and staitc_libs
			ctx.VisitDirectDepsProxies(m, func(dep android.ModuleProxy) {
				tag := ctx.OtherModuleDependencyTag(dep)
				depName := ctx.ModuleName(dep)

				// Get direct dep's Provider
				if depJavaInfo, ok := android.OtherModuleProvider(ctx, dep, java.JavaInfoProvider); ok {
					// Collect only the direct dep's own output files (non-transitive)
					depFiles := depJavaInfo.ImplementationJars.Strings()

					// Use refined property-based helpers to categorize dependencies.
					if java.IsStaticLibDepTag(tag) {
						record.StaticLibs = append(record.StaticLibs, depName)
						record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
					} else if java.IsRuntimeDepTag(tag) {
						// This will now correctly catch libTag, sdkLibTag, jniLibTag, etc.
						record.Libs = append(record.Libs, depName)
						record.LibFiles = append(record.LibFiles, depFiles...)
					}
				}
			})
		}

		if commonInfo.TestModuleInfo != nil {
			record.TestOnly = commonInfo.TestModuleInfo.TestOnly
			record.TopLevelTarget = commonInfo.TestModuleInfo.TopLevelTarget
		}

		if _, ok := android.OtherModuleProvider(ctx, m, cc.CcInfoProvider); ok {
			// Enhance BuiltFiles (retrieve more precise output paths from LinkableInfoProvider)
			if linkableInfo, ok := android.OtherModuleProvider(ctx, m, cc.LinkableInfoProvider); ok {
				if linkableInfo.OutputFile.Valid() {
					record.BuiltFiles = append(record.BuiltFiles, linkableInfo.OutputFile.Path().String())
				}
			}

			// Get direct dep's Provider
			ctx.VisitDirectDepsProxies(m, func(dep android.ModuleProxy) {
				tag := ctx.OtherModuleDependencyTag(dep)
				depName := ctx.ModuleName(dep)

				// Retrieve output paths for dependencies via LinkableInfo
				var depFiles []string
				if depLinkable, ok := android.OtherModuleProvider(ctx, dep, cc.LinkableInfoProvider); ok {
					if depLinkable.OutputFile.Valid() {
						depFiles = append(depFiles, depLinkable.OutputFile.Path().String())
					}
				}

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
			})
		}

		// Collect Rust modules information
		if _, ok := android.OtherModuleProvider(ctx, m, rust.RustInfoProvider); ok {
			ctx.VisitDirectDepsProxies(m, func(dep android.ModuleProxy) {
				tag := ctx.OtherModuleDependencyTag(dep)
				depName := ctx.ModuleName(dep)

				// Retrieve output paths via LinkableInfoProvider, as Rust shares this with other native modules.
				var depFiles []string
				if depLinkable, ok := android.OtherModuleProvider(ctx, dep, cc.LinkableInfoProvider); ok {
					if depLinkable.OutputFile.Valid() {
						depFiles = append(depFiles, depLinkable.OutputFile.Path().String())
					}
				}

				// For rust modules, treat rust_library and cc_static_lib as static deps.
				if rust.IsRlibDepTag(tag) {
					record.StaticLibs = append(record.StaticLibs, depName)
					record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
				}

				if cc.IsStaticDepTag(tag) {
					record.StaticLibs = append(record.StaticLibs, depName)
					record.StaticLibFiles = append(record.StaticLibFiles, depFiles...)
				}

				if cc.IsWholeStaticDepTag(tag) {
					record.WholeStaticLibs = append(record.WholeStaticLibs, depName)
					record.WholeStaticLibFiles = append(record.WholeStaticLibFiles, depFiles...)
				}
			})
		}

		// --- Final data deduplication and cleanup ---
		record.BuiltFiles = android.FirstUniqueStrings(record.BuiltFiles)
		record.StaticLibs = android.FirstUniqueStrings(record.StaticLibs)
		record.StaticLibFiles = android.FirstUniqueStrings(record.StaticLibFiles)
		record.Libs = android.FirstUniqueStrings(record.Libs)
		record.LibFiles = android.FirstUniqueStrings(record.LibFiles)
		record.WholeStaticLibs = android.FirstUniqueStrings(record.WholeStaticLibs)
		record.WholeStaticLibFiles = android.FirstUniqueStrings(record.WholeStaticLibFiles)
		record.HeaderLibs = android.FirstUniqueStrings(record.HeaderLibs)

		records = append(records, record)
	})

	// Serialize the records into JSON format in memory.
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
