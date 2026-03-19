// Copyright (C) 2022 The Android Open Source Project
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

package filesystem

import (
	"fmt"
	"io"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
)

//go:generate go run ../../blueprint/gobtools/codegen

var (
	toRawBinary = pctx.AndroidStaticRule("toRawBinary",
		blueprint.RuleParams{
			Command2: blueprint.NewCommand(
				"${objcopy}", ` --output-target=binary ${in} ${out} && `,
				android.Chmod, ` -x ${out}`,
			),
			CommandDeps: []string{"$objcopy"},
		},
		"objcopy")
)

func init() {
	pctx.Import("android/soong/cc/config")
}

// @auto-generate: gob
type RawBinaryInfo struct {
	// symbols info of the raw binary src
	SrcSymbolInfos android.SymbolicOutputInfos
}

var RawBinaryInfoProvider = blueprint.NewProvider[RawBinaryInfo]()

type rawBinary struct {
	android.ModuleBase

	properties rawBinaryProperties

	output     android.Path
	installDir android.InstallPath

	symbolsInfo android.SymbolicOutputInfos
}

type rawBinaryProperties struct {
	// Set the name of the output. Defaults to <module_name>.bin.
	Stem *string

	// Name of input executable. Can be a name of a target.
	Src *string `android:"path,arch_variant"`
}

func rawBinaryFactory() android.Module {
	module := &rawBinary{}
	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	return module
}

func (r *rawBinary) DepsMutator(ctx android.BottomUpMutatorContext) {
	// do nothing
}

func (r *rawBinary) installFileName() string {
	return proptools.StringDefault(r.properties.Stem, r.BaseModuleName()+".bin")
}

func (r *rawBinary) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	inputFile := android.PathForModuleSrc(ctx, proptools.String(r.properties.Src))
	outputFile := android.PathForModuleOut(ctx, r.installFileName())

	ctx.Build(pctx, android.BuildParams{
		Rule:        toRawBinary,
		Description: "raw binary " + outputFile.Base(),
		Output:      outputFile,
		Input:       inputFile,
		Args: map[string]string{
			"objcopy": "${config.ClangBin}/llvm-objcopy",
		},
	})

	r.installDir = android.PathForModuleInstall(ctx, "etc")
	ctx.InstallFile(r.installDir, r.installFileName(), outputFile)

	ctx.SetOutputFiles([]android.Path{outputFile}, "")
	r.output = outputFile

	setCommonFilesystemInfo(ctx, r)

	ctx.VisitDirectDepsProxy(func(proxy android.ModuleProxy) {
		// Find the src module
		if android.IsSourceDepTagWithOutputTag(ctx.OtherModuleDependencyTag(proxy), "") {
			if info, ok := android.OtherModuleProvider(ctx, proxy, android.CommonModuleInfoProvider); ok && info.SymbolicOutput != nil {
				r.symbolsInfo = append(r.symbolsInfo, *info.SymbolicOutput...)
			}
		}
	})

	android.SetProvider(ctx, RawBinaryInfoProvider, RawBinaryInfo{
		SrcSymbolInfos: r.symbolsInfo,
	})
}

var _ android.AndroidMkEntriesProvider = (*rawBinary)(nil)

// Implements android.AndroidMkEntriesProvider
func (r *rawBinary) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{{
		Class:      "ETC",
		OutputFile: android.OptionalPathForPath(r.output),
		ExtraFooters: []android.AndroidMkExtraFootersFunc{
			func(w io.Writer, name, prefix, moduleDir string) {
				if r.symbolsInfo != nil {
					fmt.Fprintf(w, "ALL_MODULES.$(my_register_name).SYMBOLIC_OUTPUT_PATH := %s\n", strings.Join(r.symbolsInfo.SortedUniqueSymbolicOutputPaths().Strings(), " "))
					fmt.Fprintf(w, "ALL_MODULES.$(my_register_name).ELF_SYMBOL_MAPPING_PATH := %s\n", strings.Join(r.symbolsInfo.SortedUniqueElfMappingProtoPaths().Strings(), " "))
					for _, symbol := range r.symbolsInfo.SortedUniqueSymbolicOutputPaths().Strings() {
						fmt.Fprintf(w, "INSTALLED_SYMBOLS.%s := true\n", symbol)
					}
				}
			},
		}},
	}
}

var _ Filesystem = (*rawBinary)(nil)

func (r *rawBinary) OutputPath() android.Path {
	return r.output
}

func (r *rawBinary) SignedOutputPath() android.Path {
	return nil
}
