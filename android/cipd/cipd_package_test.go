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
	"android/soong/android"
	"slices"
	"testing"
)

func TestCipdPackagePrefix_SuffixMissing(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package_prefix: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: package_suffix must not be empty'",
		ensureFile.Args["error"])
}

func TestCipdPackagePrefix_SuffixEmpty(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package_prefix: "android/prebuilts/package1",
		package_suffix: "",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: package_suffix must not be empty'",
		ensureFile.Args["error"])
}

func TestCipdPackagePrefixAndSuffix(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package_prefix: "android/prebuilts/package1",
		package_suffix: select(release_flag("PACKAGE_FLAG"), {
			default: "variant",
		}),
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)

	module := result.ModuleForTests(t, "cipd_package1", "")
	export := module.Rule("cipd_export")

	intermediateDir := "out/soong/.intermediates/cipd_package1"
	wantEnsureFile := intermediateDir + "/ensure.txt"
	if export.Input.String() != wantEnsureFile {
		t.Errorf("export.Input.String() = %v, want %v", export.Input.String(), wantEnsureFile)
	}
	if len(export.Inputs) != 0 {
		t.Errorf("len(export.Inputs) = %v, want 0", len(export.Inputs))
	}

	wantRoot := intermediateDir + "/package"
	wantExportOutputs := []string{
		wantRoot + "/package1_file1",
		wantRoot + "/package1_file2",
	}
	wantPackage := "android/prebuilts/package1/variant"
	wantVersion := "version1"

	var gotExportOutputs []string
	for _, output := range export.Outputs {
		gotExportOutputs = append(gotExportOutputs, output.String())
	}
	if !slices.Equal(wantExportOutputs, gotExportOutputs) {
		t.Errorf("export.Outputs = %v, want %v", gotExportOutputs, wantExportOutputs)
	}
	if export.Output != nil {
		t.Errorf("export.Output = %v, want nil", export.Output)
	}
	if export.Args["root"] != wantRoot {
		t.Errorf("export.Args[\"root\"] = %v, want %v", export.Args["root"], wantRoot)
	}
	if export.Args["package"] != wantPackage {
		t.Errorf("export.Args[\"package\"] = %v, want %v", export.Args["package"], wantPackage)
	}
	if export.Args["version"] != wantVersion {
		t.Errorf("export.Args[\"version\"] = %v, want %v", export.Args["version"], wantVersion)
	}

	zipRule := module.Rule("soong_zip_from_dir")
	wantZipFile := intermediateDir + "/package.zip"
	if zipRule.Output.String() != wantZipFile {
		t.Errorf("zipRule.Output = %q, want %q", zipRule.Output.String(), wantZipFile)
	}

	if zipRule.Input.String() != wantEnsureFile {
		t.Errorf("zipRule.Input.String() = %q, want %q", zipRule.Input.String(), wantEnsureFile)
	}
	wantTempZipDir := intermediateDir + "/zip_temp_pkg_dir"
	if zipRule.Args["tempZipDir"] != wantTempZipDir {
		t.Errorf("zipRule.Args[\"tempZipDir\"] = %q, want %q", zipRule.Args["tempZipDir"], wantTempZipDir)
	}
	if export.Args["package"] != wantPackage {
		t.Errorf("export.Args[\"package\"] = %v, want %v", export.Args["package"], wantPackage)
	}
	if export.Args["version"] != wantVersion {
		t.Errorf("export.Args[\"version\"] = %v, want %v", export.Args["version"], wantVersion)
	}

	zipTaggedOutputs := module.OutputFiles(result.TestContext, t, ".zip")
	if len(zipTaggedOutputs) != 1 {
		t.Errorf("len(module.OutputFiles(..., \".zip\")) = %d, want 1", len(zipTaggedOutputs))
	}
	if val := zipTaggedOutputs[0].String(); val != wantZipFile {
		t.Errorf("module.OutputFiles(..., \".zip\")[0] = %q, want %q", val, wantZipFile)
	}
}

func TestCipdPackage_NoVariant(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)

	module := result.ModuleForTests(t, "cipd_package1", "")
	export := module.Rule("cipd_export")

	intermediateDir := "out/soong/.intermediates/cipd_package1"
	wantEnsureFile := intermediateDir + "/ensure.txt"
	if export.Input.String() != wantEnsureFile {
		t.Errorf("export.Input.String() = %v, want %v", export.Input.String(), wantEnsureFile)
	}
	if len(export.Inputs) != 0 {
		t.Errorf("len(export.Inputs) = %v, want 0", len(export.Inputs))
	}

	wantRoot := intermediateDir + "/package"
	wantExportOutputs := []string{
		wantRoot + "/package1_file1",
		wantRoot + "/package1_file2",
	}
	wantPackage := "android/prebuilts/package1"
	wantVersion := "version1"

	var gotExportOutputs []string
	for _, output := range export.Outputs {
		gotExportOutputs = append(gotExportOutputs, output.String())
	}
	if !slices.Equal(wantExportOutputs, gotExportOutputs) {
		t.Errorf("export.Outputs = %v, want %v", gotExportOutputs, wantExportOutputs)
	}
	if export.Output != nil {
		t.Errorf("export.Output = %v, want nil", export.Output)
	}
	if export.Args["root"] != wantRoot {
		t.Errorf("export.Args[\"root\"] = %v, want %v", export.Args["root"], wantRoot)
	}
	if export.Args["package"] != wantPackage {
		t.Errorf("export.Args[\"package\"] = %v, want %v", export.Args["package"], wantPackage)
	}
	if export.Args["version"] != wantVersion {
		t.Errorf("export.Args[\"version\"] = %v, want %v", export.Args["version"], wantVersion)
	}

	zipRule := module.Rule("soong_zip_from_dir")
	wantZipFile := intermediateDir + "/package.zip"
	if zipRule.Output.String() != wantZipFile {
		t.Errorf("zipRule.Output = %q, want %q", zipRule.Output.String(), wantZipFile)
	}

	if zipRule.Input.String() != wantEnsureFile {
		t.Errorf("zipRule.Input.String() = %q, want %q", zipRule.Input.String(), wantEnsureFile)
	}
	wantTempZipDir := intermediateDir + "/zip_temp_pkg_dir"
	if zipRule.Args["tempZipDir"] != wantTempZipDir {
		t.Errorf("zipRule.Args[\"tempZipDir\"] = %q, want %q", zipRule.Args["tempZipDir"], wantTempZipDir)
	}
	if export.Args["package"] != wantPackage {
		t.Errorf("export.Args[\"package\"] = %v, want %v", export.Args["package"], wantPackage)
	}
	if export.Args["version"] != wantVersion {
		t.Errorf("export.Args[\"version\"] = %v, want %v", export.Args["version"], wantVersion)
	}

	zipTaggedOutputs := module.OutputFiles(result.TestContext, t, ".zip")
	if len(zipTaggedOutputs) != 1 {
		t.Errorf("len(module.OutputFiles(..., \".zip\")) = %d, want 1", len(zipTaggedOutputs))
	}
	if val := zipTaggedOutputs[0].String(); val != wantZipFile {
		t.Errorf("module.OutputFiles(..., \".zip\")[0] = %q, want %q", val, wantZipFile)
	}
}

func TestNoMatchedSelectCase_PackageSuffix(t *testing.T) {
	// Test that the bp is evaluated successfully even if there is no
	// matching select case for the "package_suffix" property.
	// It should yield an ErrorRule.
	bp := `
	cipd_package {
		name: "cipd_package1",
                package_prefix: "android/prebuilts/package1",
		package_suffix: select(release_flag("PACKAGE_FLAG"), {
			"unused": "value",
		}),
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: release_flag(\"PACKAGE_FLAG\") had value undefined, which was not handled by the select statement'",
		ensureFile.Args["error"])
}

func TestNoMatchedSelectCase_Package_NoVariant(t *testing.T) {
	// Test that the bp is evaluated successfully even if there is no
	// matching select case for the "package" property.
	// It should yield an ErrorRule.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: select(release_flag("PACKAGE_FLAG"), {
			"unused": "android/prebuilts/package1",
		}),
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: release_flag(\"PACKAGE_FLAG\") had value undefined, which was not handled by the select statement'",
		ensureFile.Args["error"])
}

func TestNoMatchedSelectCase_Version(t *testing.T) {
	// Test that the bp is evaluated successfully even if there is no
	// matching select case for the "version" property.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: select(release_flag("VERSION"), {
			"unused": "version1",
		}),
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: release_flag(\"VERSION\") had value undefined, which was not handled by the select statement'",
		ensureFile.Args["error"])
}

func TestPackageSuffixIsUnset(t *testing.T) {
	// Don't panic if the package_suffix property is unset.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package_prefix: "android/prebuilts/package1",
		package_suffix: select(release_flag("PACKAGE_FLAG"), {
			"unused": "value",
			default: unset,
		}),
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: package_suffix must not be empty'",
		ensureFile.Args["error"])
}

func TestPackageIsUnset_NoVariant(t *testing.T) {
	// Don't panic if the package property is unset.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: select(release_flag("PACKAGE_FLAG"), {
			"unused": "android/prebuilts/package1",
			default: unset,
		}),
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: package property is empty'",
		ensureFile.Args["error"])
}

func TestVersionIsUnset(t *testing.T) {
	// Don't panic if the version property is unset.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: select(release_flag("VERSION"), {
			"unused": "version1",
			default: unset,
		}),
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	ensureFile := module.Output("ensure.txt")
	if !android.IsErrorRule(ensureFile.Rule) {
		t.Errorf("Expected ErrorRule, got %q", ensureFile.Rule)
	}
	android.AssertStringEquals(t, "error message",
		"'cipd_package1: version property is empty'",
		ensureFile.Args["error"])
}

func TestCipdPackage_FilesSelect(t *testing.T) {
	// Test that select() can be used in the files property.
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		version: "version1",
		files: select(soong_config_variable("test", "var"), {
			any @ v: ["file1-" + v],
		}),
		resolved_versions_file: "cipd.versions",
	}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.VendorVars = map[string]map[string]string{
				"test": {
					"var": "debug",
				},
			}
		}),
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).RunTestWithBp(t, bp)
	module := result.ModuleForTests(t, "cipd_package1", "")
	export := module.Rule("cipd_export")
	intermediateDir := "out/soong/.intermediates/cipd_package1"
	wantEnsureFile := intermediateDir + "/ensure.txt"
	if export.Input.String() != wantEnsureFile {
		t.Errorf("export.Input.String() = %v, want %v", export.Input.String(), wantEnsureFile)
	}
	if len(export.Inputs) != 0 {
		t.Errorf("len(export.Inputs) = %v, want 0", len(export.Inputs))
	}
	wantRoot := intermediateDir + "/package"
	wantExportOutputs := []string{
		wantRoot + "/file1-debug",
	}
	wantPackage := "android/prebuilts/package1"
	wantVersion := "version1"
	var gotExportOutputs []string
	for _, output := range export.Outputs {
		gotExportOutputs = append(gotExportOutputs, output.String())
	}
	if !slices.Equal(wantExportOutputs, gotExportOutputs) {
		t.Errorf("export.Outputs = %v, want %v", gotExportOutputs, wantExportOutputs)
	}
	if export.Output != nil {
		t.Errorf("export.Output = %v, want nil", export.Output)
	}
	if export.Args["root"] != wantRoot {
		t.Errorf("export.Args[\"root\"] = %v, want %v", export.Args["root"], wantRoot)
	}
	if export.Args["package"] != wantPackage {
		t.Errorf("export.Args[\"package\"] = %v, want %v", export.Args["package"], wantPackage)
	}
	if export.Args["version"] != wantVersion {
		t.Errorf("export.Args[\"version\"] = %v, want %v", export.Args["version"], wantVersion)
	}
}

func TestCipdPackage_CantMixPackageAndPackagePrefix(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		package_prefix: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).
		ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern("package: cannot be specified together with package_prefix")).
		RunTestWithBp(t, bp)
}

func TestCipdPackage_CantMixPackageAndPackageSuffix(t *testing.T) {
	bp := `
	cipd_package {
		name: "cipd_package1",
		package: "android/prebuilts/package1",
		package_suffix: "android/prebuilts/package1",
		version: "version1",
		files: [
			"package1_file1",
			"package1_file2",
		],
		resolved_versions_file: "cipd.versions",
	}
	`

	android.GroupFixturePreparers(
		android.PrepareForTestWithAndroidBuildComponents,
		android.FixtureRegisterWithContext(RegisterCipdPackageComponents),
	).
		ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern("must not specify both package and package_suffix")).
		RunTestWithBp(t, bp)
}
