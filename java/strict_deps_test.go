// Copyright 2026 Google Inc. All rights reserved.
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

// Tests for strict dependencies enforcement in Java modules.
package java

import (
	"android/soong/android"
	"strings"
	"testing"
)

func TestStrictDeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string // "warn", "error", "off", or ""
	}{
		{
			name:  "warn",
			value: "warn",
		},
		{
			name:  "error",
			value: "error",
		},
		{
			name:  "off",
			value: "off",
		},
		{
			name:  "omitted",
			value: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var strictDepsStr string
			if tc.value != "" {
				strictDepsStr = `strict_deps: "` + tc.value + `",`
			}
			strictDepsEnabled := tc.value == "warn" || tc.value == "error"

			result := android.GroupFixturePreparers(
				prepareForJavaTest,
			).RunTestWithBp(t, `
				java_library {
					name: "foo",
					srcs: ["a.java"],
					libs: ["bar"],
					`+strictDepsStr+`
				}

				java_library {
					name: "bar",
					srcs: ["b.java"],
					static_libs: ["baz"],
				}

				java_library {
					name: "baz",
					srcs: ["c.java"],
				}

				java_plugin {
					name: "soong_java_strict_deps_plugin",
					srcs: ["plugin.java"],
				}

				kotlin_plugin {
					name: "soong_kotlin_strict_deps_plugin",
					srcs: ["plugin.java"],
				}
			`)

			foo := result.ModuleForTests(t, "foo", "android_common").Module()

			// Verify that strict deps plugins are added as dependencies
			if strictDepsEnabled {
				hasPlugin := false
				hasKotlinPlugin := false
				result.VisitDirectDeps(foo, func(dep android.Module) {
					name := result.ModuleName(dep)
					if name == "soong_java_strict_deps_plugin" {
						hasPlugin = true
					}
					if name == "soong_kotlin_strict_deps_plugin" {
						hasKotlinPlugin = true
					}
				})
				android.AssertBoolEquals(t, "soong_java_strict_deps_plugin dependency", true, hasPlugin)
				android.AssertBoolEquals(t, "soong_kotlin_strict_deps_plugin dependency", true, hasKotlinPlugin)
			}

			// 2. Verify directClasspath contains only direct libs and excludes transitive static libs
			fooLib := foo.(*Library)
			hasBar := false
			hasTransitiveBaz := false

			classpathStrings := fooLib.directClasspath.Strings()
			t.Logf("foo directClasspath: %v", classpathStrings)

			for _, p := range classpathStrings {
				if strings.Contains(p, "bar.jar") || strings.Contains(p, "bar/android_common") {
					hasBar = true
				}
				if strings.Contains(p, "baz.jar") || strings.Contains(p, "baz/android_common") {
					hasTransitiveBaz = true
				}
			}

			if strictDepsEnabled {
				android.AssertBoolEquals(t, "directClasspath includes bar", true, hasBar)
				android.AssertBoolEquals(t, "directClasspath EXCLUDES transitive baz", false, hasTransitiveBaz)

			} else {
				// When strict_deps is off, directClasspath shouldn't be populated for injection whitelisting
				if len(classpathStrings) > 0 {
					t.Errorf("Expected directClasspath to be empty when strict_deps is off/omitted, but got: %v", classpathStrings)
				}
			}
		})
	}
}
