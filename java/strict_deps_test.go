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
					name: "foo_sharded",
					srcs: ["a.java", "b.java"],
					libs: ["bar"],
					javac_shard_size: 1,
					`+strictDepsStr+`
				}

				java_library {
					name: "foo_kotlin",
					srcs: ["a.kt"],
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

			fooLib := foo.(*Library)
			classpathStrings := fooLib.directClasspath.Strings()

			if strictDepsEnabled {
				// Verify both compilation pathways (incremental and sharded/full) generate the JavaStrictDeps plugin flag
				fooIncRule := result.ModuleForTests(t, "foo", "android_common").Rule("javac")
				fooShardedRule := result.ModuleForTests(t, "foo_sharded", "android_common").Rule("javac")

				fooIncRspPath := result.ModuleForTests(t, "foo", "android_common").Output("javac/strict_deps.rsp").Output.String()
				android.AssertStringDoesContain(t, "foo (incremental) javac flags", fooIncRule.Args["javacFlags"], "-Xplugin:\"JavaStrictDeps "+fooIncRspPath+" "+tc.value+"\"")

				fooShardedRspPath := result.ModuleForTests(t, "foo_sharded", "android_common").Output("javac/shard0/strict_deps.rsp").Output.String()
				android.AssertStringDoesContain(t, "foo_sharded (full/sharded) javac flags", fooShardedRule.Args["javacFlags"], "-Xplugin:\"JavaStrictDeps "+fooShardedRspPath+" "+tc.value+"\"")

				fooIncRsp := android.ContentFromFileRuleForTests(t, result.TestContext, result.ModuleForTests(t, "foo", "android_common").Output("javac/strict_deps.rsp"))
				android.AssertStringDoesContain(t, "foo (incremental) rsp file contents include bar", fooIncRsp, "bar.jar")
				android.AssertStringDoesNotContain(t, "foo (incremental) rsp file contents EXCLUDE baz", fooIncRsp, "baz.jar")

				fooKotlinRule := result.ModuleForTests(t, "foo_kotlin", "android_common").Rule("kotlinc")
				fooKotlinRspPath := result.ModuleForTests(t, "foo_kotlin", "android_common").Output("strict_deps.rsp").Output.String()
				android.AssertStringDoesContain(t, "foo_kotlin kotlinc flags (-Xplugin)", fooKotlinRule.Args["kotlincFlags"], "-Xplugin=")
				android.AssertStringDoesContain(t, "foo_kotlin kotlinc flags (-P rsp)", fooKotlinRule.Args["kotlincFlags"], "-P plugin:com.android.strictdeps:rsp="+fooKotlinRspPath)
				android.AssertStringDoesContain(t, "foo_kotlin kotlinc flags (-P level)", fooKotlinRule.Args["kotlincFlags"], "-P plugin:com.android.strictdeps:level="+tc.value)
			} else {
				// When strict_deps is off, directClasspath shouldn't be populated for injection whitelisting
				if len(classpathStrings) > 0 {
					t.Errorf("Expected directClasspath to be empty when strict_deps is off/omitted, but got: %v", classpathStrings)
				}

				fooIncRule := result.ModuleForTests(t, "foo", "android_common").Rule("javac")
				fooShardedRule := result.ModuleForTests(t, "foo_sharded", "android_common").Rule("javac")

				android.AssertStringDoesNotContain(t, "foo (incremental) javac flags should NOT have plugin", fooIncRule.Args["javacFlags"], "-Xplugin:\"JavaStrictDeps")
				android.AssertStringDoesNotContain(t, "foo_sharded javac flags should NOT have plugin", fooShardedRule.Args["javacFlags"], "-Xplugin:\"JavaStrictDeps")

				fooKotlinRule := result.ModuleForTests(t, "foo_kotlin", "android_common").Rule("kotlinc")
				android.AssertStringDoesNotContain(t, "foo_kotlin kotlinc flags should NOT have plugin", fooKotlinRule.Args["kotlincFlags"], "-Xplugin=")
			}
		})
	}
}

func TestStrictDepsWrapper(t *testing.T) {
	t.Parallel()

	result := android.GroupFixturePreparers(
		prepareForJavaTest,
	).RunTestWithBp(t, `
		java_library {
			name: "foo",
			srcs: ["a.java"],
			libs: ["bar_wrapper"],
			strict_deps: "error",
		}

		java_library {
			name: "bar_wrapper",
			libs: ["bar"],
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

	fooIncRule := result.ModuleForTests(t, "foo", "android_common").Rule("javac")
	fooIncRspPath := result.ModuleForTests(t, "foo", "android_common").Output("javac/strict_deps.rsp").Output.String()
	android.AssertStringDoesContain(t, "foo javac flags for wrapper test", fooIncRule.Args["javacFlags"], "-Xplugin:\"JavaStrictDeps "+fooIncRspPath+" error\"")
}
