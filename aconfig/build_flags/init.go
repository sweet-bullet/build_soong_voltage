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

package build_flags

import (
	"android/soong/android"

	"github.com/google/blueprint"
)

var (
	pctx = android.NewPackageContext("android/soong/aconfig/build_flags")

	buildFlagInternal          = pctx.HostTool("build-flag-internal")
	buildFlagDeclarations      = pctx.HostTool("build-flag-declarations")
	releaseConfigInternal      = pctx.HostTool("release-config-internal")
	releaseConfigContributions = pctx.HostTool("release-config-contributions")
	cpIfChanged                = android.CpIfChanged

	// For build_flag_declarations: Generate cache file
	buildFlagRule = pctx.AndroidStaticRule("build-flag-declarations",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				buildFlagDeclarations,
				` --top .`,
				` ${declarations}`,
				` --format pb`,
				` --output ${out}.tmp && `,
				cpIfChanged, ` ${out}.tmp ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		}, "release_version", "declarations")

	buildFlagTextRule = pctx.AndroidStaticRule("build-flag-declarations-text",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				buildFlagDeclarations,
				` --top .`,
				` --format=textproto`,
				` --intermediate ${in}`,
				` --format textproto`,
				` --output ${out}.tmp && `,
				cpIfChanged, ` ${out}.tmp ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		})

	allDeclarationsRule = pctx.AndroidStaticRule("all-build-flag-declarations-dump",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				buildFlagDeclarations,
				` --top .`,
				` ${intermediates}`,
				` --format pb`,
				` --output ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		}, "intermediates")

	allDeclarationsRuleTextProto = pctx.AndroidStaticRule("All_build_flag_declarations_dump_textproto",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				buildFlagDeclarations,
				` --top .`,
				` --intermediate ${in}`,
				` --format textproto`,
				` --output ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		})

	allReleaseConfigsRule = pctx.AndroidStaticRule("All_release_configs",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				releaseConfigInternal,
				" --top .",
				" --quiet `", android.Cat, " ${argsFile}`",
				" --out_dir ${moduleOut}",
				" --pb --textproto --json --inheritance",
			),
			Restat:          true,
			SandboxDisabled: true,
		}, "argsFile", "moduleOut", "product")

	releaseConfigRule = pctx.AndroidStaticRule("Release_config",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				releaseConfigInternal,
				" --top .",
				" --quiet `", android.Cat, " ${argsFile}`",
				" --out_dir ${moduleOut}",
				" --container",
			),
			Restat:          true,
			SandboxDisabled: true,
		}, "argsFile", "moduleOut", "product")

	allReleaseConfigContributionsRule = pctx.AndroidStaticRule("all-release-config-contributions-dump",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				releaseConfigContributions,
				" --top .",
				` ${dirs}`,
				` --format ${format}`,
				` --output ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		}, "dirs", "format")

	flagDeclarationsValidationRule = pctx.AndroidStaticRule("flagDeclarationsValidation",
		blueprint.RuleParams{
			// Get no flags, so that we have no output.
			Command2: blueprint.NewCommand(
				buildFlagInternal,
				` --top .`,
				` --maps-file ${in}`,
				` --quiet`,
				` --declarations-only get && date > ${out}`,
			),
			Restat:          true,
			SandboxDisabled: true,
		})
)

func init() {
	RegisterBuildComponents(android.InitRegistrationContext)
	pctx.Import("android/soong/android")
	pctx.HostBinToolVariable("buildFlagInternal", "build-flag-internal")
	pctx.HostBinToolVariable("buildFlagDeclarations", "build-flag-declarations")
	pctx.HostBinToolVariable("releaseConfigInternal", "release-config-internal")
	pctx.HostBinToolVariable("releaseConfigContributions", "release-config-contributions")
}

func RegisterBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("all_release_configs", AllReleaseConfigsFactory)
	ctx.RegisterModuleType("build_flag_declarations", DeclarationsFactory)
	ctx.RegisterModuleType("release_config", ReleaseConfigFactory)
	ctx.RegisterModuleType("release_config_contributions", ReleaseConfigContributionsFactory)
	ctx.RegisterParallelSingletonType("all_build_flag_declarations", AllBuildFlagDeclarationsFactory)
}
