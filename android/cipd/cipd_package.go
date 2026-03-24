// Copyright 2025 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cipd

import (
	"errors"
	"fmt"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

//go:generate go run ../../../blueprint/gobtools/codegen

func init() {
	RegisterCipdPackageComponents(android.InitRegistrationContext)

	pctx.VariableConfigMethod("PrebuiltOS", android.Config.PrebuiltOS)
	pctx.SourcePathVariable("cipd", "prebuilts/cipd/${PrebuiltOS}/cipd")
	pctx.HostBinToolVariable("soong_zip", "soong_zip")
}

func RegisterCipdPackageComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("cipd_package", cipdPackageFactory)
}

// @auto-generate: gob
type CipdPackageInfo struct {
	FullPackageName string
	Version         string
}

var CipdPackageInfoProvider = blueprint.NewProvider[*CipdPackageInfo]()

var (
	pctx = android.NewPackageContext("android/cipd")

	// CIPD can be expensive for network and disk i/o, so limit the number of concurrent
	// fetches.
	cipdPool = pctx.StaticPool("cipdPool", blueprint.PoolParams{
		Depth: 8,
	})
	// cipd will proxy its requests out of the build sandbox using the unix domain socket
	// set up in build/soong/ui/build/cipd.go.
	cipdExportRule = pctx.AndroidStaticRule("cipd_export",
		blueprint.RuleParams{
			Command:         "rm -rf $root && $cipd export -log-level warning  -ensure-file $in -root $root",
			CommandDeps:     []string{"$cipd"},
			Description:     "CIPD export $package@$version",
			Pool:            cipdPool,
			SandboxDisabled: true,
		}, "root", "package", "version",
	)

	soongZipFromDirRule = pctx.AndroidStaticRule("soong_zip_from_dir",
		blueprint.RuleParams{
			Command: "rm -rf $tempZipDir && " +
				"$cipd export -log-level warning -ensure-file $in -root $tempZipDir && " +
				"$soong_zip -write_if_changed -o $out -C $tempZipDir -D $tempZipDir && " +
				"rm -rf $tempZipDir",
			CommandDeps:     []string{"$cipd", "$soong_zip"},
			Description:     "CIPD export and zip $package@$version",
			Pool:            cipdPool,
			Restat:          true,
			SandboxDisabled: true,
		}, "tempZipDir", "package", "version",
	)
)

type cipdPackageProperties struct {
	// For packages with no variants, this should contain the full name of the CIPD
	// package, like "android/prebuilts/MyPrebuiltApp". For a package with
	// variants, leave this unset, and use package_prefix and package_suffix instead.
	// TODO(b/482841429): Change to non-configurable once all dynamic packages are
	// migrated to use package_prefix and package_suffix.
	Package proptools.Configurable[string]

	// For a package with variants, this should contain the static prefix for
	// the package name -- that is, the part that does not depend on any variables.
	// For example, "android/prebuilts/GmsCorePrebuit" (do not include a trailing
	// slash). For a package with no variants, leave this unset, and use "package"
	// instead.
	Package_prefix string

	// For a package with variants, this should contain the dynamic suffix for
	// the package name (that is, computed using variables). The suffix is joined
	// to package_prefix with a slash ('/') in between. For a package with no
	// variants, leave this unset, and use "package" instead.
	Package_suffix proptools.Configurable[string]

	// The version tag of the package.
	Version proptools.Configurable[string]

	// A file containing pinned cipd instance ids. It must contain the package version
	// specified.
	Resolved_versions_file string `android:"path"`

	// The files expected to exist in the CIPD package.
	Files proptools.Configurable[[]string]
}

type cipdPackageModule struct {
	android.ModuleBase

	properties cipdPackageProperties
}

// In parsing the package and version properties, we signal two types of errors:
//
//  1. evaluation-time errors mean that the soong module is malformed, such as
//     missing required properties. These are triggered using ctx.PropertyErrorf().
//
//  2. run-time errors trigger a failure only if the cipd_package build action
//     is executed. These allow for leaving "package", "package_suffix", or "version"
//     empty or unset in cases where the cipd_package will not be used (such as if it
//     is a conditional dependency). These are triggered by returning an error type,
//     which is put into an ErrorRule.

func (p *cipdPackageModule) computePackageNoVariant(ctx android.ModuleContext) (pkg string, err error) {
	packageProp, err := p.properties.Package.GetOrErr(ctx)
	if err != nil {
		return pkg, err
	}
	pkg = packageProp.GetOrDefault("")
	if len(pkg) == 0 {
		return pkg, errors.New("package property is empty")
	}
	if len(p.properties.Package_prefix) > 0 {
		ctx.PropertyErrorf("package", "must not specify both package and package_prefix")
	}
	if pkgSuffixProp, err := p.properties.Package_suffix.GetOrErr(ctx); err != nil || pkgSuffixProp.IsPresent() {
		ctx.PropertyErrorf("package", "must not specify both package and package_suffix")
	}
	return pkg, nil
}

func (p *cipdPackageModule) computePackage(ctx android.ModuleContext) (pkg string, err error) {
	packagePrefix := p.properties.Package_prefix
	if len(packagePrefix) == 0 {
		ctx.PropertyErrorf("package_prefix", "must not be empty")
	}

	if pkgProp, err := p.properties.Package.GetOrErr(ctx); err != nil || pkgProp.IsPresent() {
		ctx.PropertyErrorf("package", "cannot be specified together with package_prefix")
	}

	packageSuffixProp, err := p.properties.Package_suffix.GetOrErr(ctx)
	if err != nil {
		return pkg, err
	}
	packageSuffix := packageSuffixProp.GetOrDefault("")
	if len(packageSuffix) == 0 {
		return pkg, errors.New("package_suffix must not be empty")
	}
	return packagePrefix + "/" + packageSuffix, nil
}

func (p *cipdPackageModule) computePackageVersion(ctx android.ModuleContext) (pkg, version string, err error) {
	var errs []error
	// TODO(b/482841429): once "package" becomes non-configurable, it should
	// be an evaluation error to leave both "package" and "package_prefix"
	// unset.
	if len(p.properties.Package_prefix) > 0 {
		pkg, err = p.computePackage(ctx)
	} else {
		pkg, err = p.computePackageNoVariant(ctx)
	}
	if err != nil {
		errs = append(errs, err)
	}

	versionProp, err := p.properties.Version.GetOrErr(ctx)
	if err != nil {
		errs = append(errs, err)
	} else {
		version = versionProp.GetOrDefault("")
		if len(version) == 0 {
			errs = append(errs, errors.New("version property is empty"))
		}
	}
	return pkg, version, errors.Join(errs...)
}

func (p *cipdPackageModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	ensureFile := android.PathForModuleOut(ctx, "ensure.txt")
	outPath := android.PathForModuleOut(ctx, "package")

	// The resolved versions file should be relative to the ensure file, so
	// copy it to the output directory as well.
	const resolvedVersionsTxt = "resolved_versions.txt"
	resolvedVersionsFile := android.PathForModuleOut(ctx, resolvedVersionsTxt)
	android.CopyFileRule(ctx,
		android.PathForModuleSrc(ctx, p.properties.Resolved_versions_file),
		resolvedVersionsFile.OutputPath)

	pkg, version, err := p.computePackageVersion(ctx)
	if err != nil {
		android.ErrorRule(ctx, ensureFile, p.Name()+": "+err.Error())
		android.SetProvider(ctx, CipdPackageInfoProvider, &CipdPackageInfo{
			FullPackageName: pkg,
			Version:         version,
		})
	} else {
		android.WriteFileRule(ctx, ensureFile, fmt.Sprintf("$ResolvedVersions %s\n%s %s\n", resolvedVersionsTxt, pkg, version))
	}

	files := p.properties.Files.GetOrDefault(ctx, nil)
	if len(files) > 0 {
		outFiles := make(android.WritablePaths, len(files))
		for i, f := range files {
			outFiles[i] = outPath.Join(ctx, f)
		}

		ctx.Build(pctx, android.BuildParams{
			Rule:     cipdExportRule,
			Input:    ensureFile,
			Outputs:  outFiles,
			Implicit: resolvedVersionsFile,
			Args: map[string]string{
				"root":    outPath.String(),
				"package": pkg,
				"version": version,
			},
		})
		ctx.SetOutputFiles(outFiles.Paths(), "")
	}

	outputZipFile := android.PathForModuleOut(ctx, "package.zip")
	tempZipDir := android.PathForModuleOut(ctx, "zip_temp_pkg_dir")
	// This rule runs `cipd export` (potentially again) to ensure the zip is
	// creatabled regardless of whether individual files are also requested.
	ctx.Build(pctx, android.BuildParams{
		Rule:     soongZipFromDirRule,
		Input:    ensureFile,
		Output:   outputZipFile,
		Implicit: resolvedVersionsFile,
		Args: map[string]string{
			"tempZipDir": tempZipDir.String(),
			"package":    pkg,
			"version":    version,
		},
	})
	ctx.SetOutputFiles(android.Paths{outputZipFile}, ".zip")

	ctx.ComplianceMetadataInfo().SetStringValue(android.ComplianceMetadataProp.CIPD_VERSION, version)
}

// cipd_package module installs the given CIPD package version.
func cipdPackageFactory() android.Module {
	module := &cipdPackageModule{}
	module.AddProperties(&module.properties)
	android.InitAndroidModule(module)
	return module
}
