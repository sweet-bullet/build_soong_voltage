// Copyright 2017 Google Inc. All rights reserved.
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

package config

import "strings"
import "android/soong/android"

var (
	KotlinStdlibJar     = "external/kotlinc/lib/kotlin-stdlib.jar"
	KotlincIllegalFlags = []string{
		"-no-jdk",
		"-no-stdlib",
		"-language-version",
	}
)

func init() {
	pctx.HostBinToolVariable("KotlinIncrementalClientBinary", "kotlin-incremental-client")
	pctx.HostBinToolVariable("KotlinKspClientBinary", "kotlin-ksp-client")
	pctx.HostBinToolVariable("KotlinJarSnapshotterBinary", "kotlin-jar-snapshotter")
	pctx.SourcePathVariable("KotlincCmd", "external/kotlinc/bin/kotlinc")
	pctx.SourcePathVariable("KotlinCompilerJar", "external/kotlinc/lib/kotlin-compiler.jar")
	pctx.SourcePathVariable("KotlinPreloaderJar", "external/kotlinc/lib/kotlin-preloader.jar")
	pctx.SourcePathVariable("KotlinReflectJar", "external/kotlinc/lib/kotlin-reflect.jar")
	pctx.SourcePathVariable("KotlinScriptRuntimeJar", "external/kotlinc/lib/kotlin-script-runtime.jar")
	pctx.SourcePathVariable("KotlinTrove4jJar", "external/kotlinc/lib/trove4j.jar")
	pctx.SourcePathVariable("KotlinKaptJar", "external/kotlinc/lib/kotlin-annotation-processing.jar")
	pctx.SourcePathVariable("KotlinKaptEmbeddableJar", "external/kotlinc/lib/kotlin-annotation-processing-embeddable-2.1.10.jar")
	pctx.SourcePathVariable("KotlinAnnotationJar", "external/kotlinc/lib/annotations-13.0.jar")
	pctx.SourcePathVariable("KotlinStdlibJar", KotlinStdlibJar)
	pctx.SourcePathVariable("KotlinAbiGenPluginJar", "external/kotlinc/lib/jvm-abi-gen.jar")

	// These flags silence "Illegal reflective access" warnings when running kapt in OpenJDK9+
	pctx.StaticVariable("KaptSuppressJDK9Warnings", strings.Join([]string{
		"-J--add-exports=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED",
		"-J--add-exports=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED",
		"-J--add-exports=jdk.compiler/com.sun.tools.javac.main=ALL-UNNAMED",
		"-J--add-opens=java.base/sun.net.www.protocol.jar=ALL-UNNAMED",
	}, " "))

	pctx.StaticVariable("KotlincHeapSize", "8192M")
	pctx.StaticVariable("KotlincHeapFlags", "-J-Xmx${KotlincHeapSize}")

	// These flags silence warnings seen when running kotlinc in OpenJDK9+
	pctx.VariableFunc("KotlincSuppressJDK9Warnings", func(ctx android.PackageVarContext) string {
		suppressWarningsFlags := []string{
			// "Illegal reflective access"
			"-J--add-opens=java.base/java.util=ALL-UNNAMED", // https://youtrack.jetbrains.com/issue/KT-43704
		}

		if ctx.Config().BuildWithJdk25() {
			suppressWarningsFlags = append(suppressWarningsFlags,
				// restricted method in java.lang.System
				"-J--enable-native-access=ALL-UNNAMED", // b/445166084
				// deprecated sun.misc.Unsafe::objectFieldOffset
				"-J--sun-misc-unsafe-memory-access=allow", // b/445162494, https://youtrack.jetbrains.com/issue/IJPL-191435
				// annotation applied to the value parameter only
				"-Xannotation-default-target=first-only", // b/445163990, https://youtrack.jetbrains.com/issue/KT-73255
			)
		}
		return strings.Join(suppressWarningsFlags, " ")
	})

	pctx.StaticVariable("KotlincGlobalFlags", strings.Join([]string{}, " "))
	// Use KotlincKytheGlobalFlags to prevent kotlinc version skew issues between android and
	// g3 kythe indexers.
	// This is necessary because there might be instances of kotlin code in android
	// platform that are not fully compatible with the kotlinc used in g3 kythe indexers.
	// e.g. uninitialized variables are a warning in 1.*, but an error in 2.*
	// https://github.com/JetBrains/kotlin/blob/master/compiler/fir/checkers/gen/org/jetbrains/kotlin/fir/analysis/diagnostics/FirErrors.kt#L748
	pctx.StaticVariable("KotlincKytheGlobalFlags", strings.Join([]string{}, " "))
}
