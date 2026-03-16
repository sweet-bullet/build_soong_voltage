// Copyright 2018 Google Inc. All rights reserved.
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

package java

import (
	"fmt"
	"testing"

	"android/soong/android"
)

func AssertJarJarRename(t *testing.T, result *android.TestResult, libName, original, expectedRename string) {
	module := result.ModuleForTests(t, libName, "android_common")

	provider, found := android.OtherModuleProvider(result.OtherModuleProviderAdaptor(), module.Module(), JarJarProvider)
	android.AssertBoolEquals(t, fmt.Sprintf("found provider (%s)", libName), true, found)

	renamed, found := provider.Rename[original]
	android.AssertBoolEquals(t, fmt.Sprintf("found rename (%s)", libName), true, found)
	android.AssertStringEquals(t, fmt.Sprintf("renamed (%s)", libName), expectedRename, renamed)
}

func TestJarJarRenameDifferentModules(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
	).RunTestWithBp(t, `
		java_library {
			name: "their_lib",
			jarjar_rename: ["com.example.a"],
		}

		java_library {
			name: "boundary_lib",
			jarjar_prefix: "RENAME",
			static_libs: ["their_lib"],
		}

		java_library {
			name: "my_lib",
			static_libs: ["boundary_lib"],
		}
	`)

	original := "com.example.a"
	renamed := "RENAME.com.example.a"
	AssertJarJarRename(t, result, "their_lib", original, "")
	AssertJarJarRename(t, result, "boundary_lib", original, renamed)
	AssertJarJarRename(t, result, "my_lib", original, renamed)
}

func TestJarJarRenameSameModule(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
	).RunTestWithBp(t, `
		java_library {
			name: "their_lib",
			jarjar_rename: ["com.example.a"],
			jarjar_prefix: "RENAME",
		}

		java_library {
			name: "my_lib",
			static_libs: ["their_lib"],
		}
	`)

	original := "com.example.a"
	renamed := "RENAME.com.example.a"
	AssertJarJarRename(t, result, "their_lib", original, renamed)
	AssertJarJarRename(t, result, "my_lib", original, renamed)
}

// Only manual jarjar rules will merge dependencies, ordinary repackaging should not.
func TestThinHeaderJarOptimization(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		android.FixtureAddTextFile("rules.txt", "rule com.example.** RENAME.@1\n"),
	).RunTestWithBp(t, `
		java_library {
			name: "dep",
			srcs: ["a.java"],
		}

		java_library {
			name: "lib_thin",
			srcs: ["b.java"],
			static_libs: ["dep"],
		}

		java_library {
			name: "lib_jarjar_rules",
			srcs: ["c.java"],
			static_libs: ["dep"],
			jarjar_rules: "rules.txt",
		}

		java_library {
			name: "lib_repackage",
			srcs: ["d.java"],
			static_libs: ["dep"],
			jarjar_rename: ["com.example.Dep"],
			jarjar_prefix: "some_prefix",
		}
	`)

	// ------------------------------------------------------------------
	// Scenario 1: Thin-Jar (No jarjar_rules)
	// ------------------------------------------------------------------
	libThin := result.ModuleForTests(t, "lib_thin", "android_common")
	libThinInfo, found := android.OtherModuleProvider(result.OtherModuleProviderAdaptor(), libThin.Module(), JavaInfoProvider)
	android.AssertBoolEquals(t, "found JavaInfoProvider for lib_thin", true, found)

	// Expect TransitiveStaticLibsHeaderJars to be POPULATED (thin-jar preserves tracking)
	thinList := libThinInfo.TransitiveStaticLibsHeaderJars.ToList()
	android.AssertIntEquals(t, "lib_thin TransitiveStaticLibsHeaderJars length", 2, len(thinList))
	android.AssertStringEquals(t, "lib_thin item 0", "lib_thin.jar", thinList[0].Base())
	android.AssertStringEquals(t, "lib_thin item 1", "dep.jar", thinList[1].Base())

	// ------------------------------------------------------------------
	// Scenario 2: Fat-Jar (With jarjar_rules)
	// ------------------------------------------------------------------
	libJarjarRules := result.ModuleForTests(t, "lib_jarjar_rules", "android_common")
	libJarjarRulesInfo, found := android.OtherModuleProvider(result.OtherModuleProviderAdaptor(), libJarjarRules.Module(), JavaInfoProvider)
	android.AssertBoolEquals(t, "found JavaInfoProvider for lib_jarjar_rules", true, found)

	// In the fat case, transitiveStaticLibsHeaderJars is assigned to nil in base.go.
	// Its aggregate Transitive list contains ONLY its own 1 repackaged node output node (excluding dep).
	fatList := libJarjarRulesInfo.TransitiveStaticLibsHeaderJars.ToList()
	android.AssertIntEquals(t, "lib_jarjar_rules TransitiveStaticLibsHeaderJars length (fat-jar)", 1, len(fatList))
	android.AssertStringEquals(t, "lib_jarjar_rules item 0", "lib_jarjar_rules.jar", fatList[0].Base())

	// ------------------------------------------------------------------
	// Scenario 3: Repackaging (jarjar_prefix)
	// ------------------------------------------------------------------
	libRepackage := result.ModuleForTests(t, "lib_repackage", "android_common")
	libRepackageInfo, found := android.OtherModuleProvider(result.OtherModuleProviderAdaptor(), libRepackage.Module(), JavaInfoProvider)
	android.AssertBoolEquals(t, "found JavaInfoProvider for lib_repackage", true, found)

	// In the repackaging case, it should NOT break the thin list chain!
	repackageList := libRepackageInfo.TransitiveStaticLibsHeaderJars.ToList()
	android.AssertIntEquals(t, "lib_repackage TransitiveStaticLibsHeaderJars length", 3, len(repackageList))
	android.AssertStringEquals(t, "lib_repackage item 0", "lib_repackage.jar", repackageList[0].Base())
	android.AssertStringEquals(t, "lib_repackage item 1", "lib_repackage.0.jar", repackageList[1].Base())
	android.AssertStringEquals(t, "lib_repackage item 2", "dep.jar", repackageList[2].Base())

	// TODO(b/356688296): this shouldn't export both the unmodified and repackaged header jars.
	// We assert 2 items here only because of this known limitation.
	android.AssertIntEquals(t, "lib_repackage LocalHeaderJars length", 2, len(libRepackageInfo.LocalHeaderJars))
}
