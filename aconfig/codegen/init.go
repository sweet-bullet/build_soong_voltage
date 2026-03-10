// Copyright 2023 Google Inc. All rights reserved.
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

package codegen

import (
	"android/soong/aconfig"
	"android/soong/android"

	"github.com/google/blueprint"
)

var (
	pctx        = android.NewPackageContext("android/soong/aconfig/codegen")
	rm          = android.Rm
	mkdir       = android.Mkdir
	aconfigTool = aconfig.Aconfig
	soongZip    = android.SoongZip

	// For java_aconfig_library: Generate java library
	javaRule = pctx.AndroidStaticRule("java_aconfig_library",
		blueprint.RuleParams{
			// LINT.IfChange
			Command2: blueprint.NewCommand(
				rm, ` -rf ${out}.tmp && `,
				mkdir, ` -p ${out}.tmp && `,
				aconfigTool, ` create-java-lib`,
				`    --mode ${mode}`,
				`    --allow-impl-interface-removal ${allow_impl_interface_removal}`,
				`    --cache ${in}`,
				`    --out ${out}.tmp && `,
				soongZip, ` -write_if_changed -jar -o ${out} -C ${out}.tmp -D ${out}.tmp && `,
				rm, ` -rf ${out}.tmp`,
			),
			// LINT.ThenChange(/aconfig/init.go)
			Restat: true,
		}, "mode", "allow_impl_interface_removal")

	// For cc_aconfig_library: Generate C++ library
	cppRule = pctx.AndroidStaticRule("cc_aconfig_library",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				rm, ` -rf ${gendir} && `,
				mkdir, ` -p ${gendir} && `,
				aconfigTool, ` create-cpp-lib`,
				`    --mode ${mode}`,
				`    --cache ${in}`,
				`    --out ${gendir}`,
			),
		}, "gendir", "mode")

	// For rust_aconfig_library: Generate Rust library
	rustRule = pctx.AndroidStaticRule("rust_aconfig_library",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				rm, ` -rf ${gendir} && `,
				mkdir, ` -p ${gendir} && `,
				aconfigTool, ` create-rust-lib`,
				`    --mode ${mode}`,
				`    --cache ${in}`,
				`    --out ${gendir}`,
			),
		}, "gendir", "mode")
)

func init() {
	RegisterBuildComponents(android.InitRegistrationContext)
	pctx.HostBinToolVariable("aconfig", "aconfig")
	pctx.HostBinToolVariable("soong_zip", "soong_zip")
}

func RegisterBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("aconfig_declarations_group", AconfigDeclarationsGroupFactory)
	ctx.RegisterModuleType("cc_aconfig_library", CcAconfigLibraryFactory)
	ctx.RegisterModuleType("java_aconfig_library", JavaDeclarationsLibraryFactory)
	ctx.RegisterModuleType("rust_aconfig_library", RustAconfigLibraryFactory)
}
