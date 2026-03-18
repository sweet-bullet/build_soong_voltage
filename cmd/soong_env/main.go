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

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

func newMultiString(name, usage string) *multiString {
	var f multiString
	flag.Var(&f, name, usage)
	return &f
}

type multiString []string

func (ms *multiString) String() string     { return strings.Join(*ms, ", ") }
func (ms *multiString) Set(s string) error { *ms = append(*ms, s); return nil }

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s -i [--allow VAR]... [NAME=VALUE]... <command> [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  This is a reimplementation of the POSIX env command, but with\n")
		fmt.Fprintf(os.Stderr, "  a new --allow flag to allow certain environment variables even\n")
		fmt.Fprintf(os.Stderr, "  when using -i.\n")
		flag.PrintDefaults()
	}
	isolate := flag.Bool("i", false, "Remove all existing environment variables, only keeping the ones "+
		"specified on the command line. This is required now because it's probably what you want, "+
		"and having it vs it being default true keeps this tool consistent with POSIX env.")
	allow := newMultiString("allow", "Copy these environment variables from the original environement "+
		"into a -i isolated environment.")
	flag.Parse()

	if isolate == nil || *isolate == false {
		fmt.Fprintf(os.Stderr, "error: -i flag is required\n")
		os.Exit(1)
	}

	for _, env := range os.Environ() {
		v, _, found := strings.Cut(env, "=")
		if !found {
			fmt.Fprintf(os.Stderr, "Failed to find = in environment variable %q\n", env)
			os.Exit(1)
		}
		if !slices.Contains(*allow, v) {
			if err := os.Unsetenv(v); err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
		}
	}

	startedCommand := false
	var cmdArgs []string
	for _, arg := range flag.Args() {
		name, val, found := strings.Cut(arg, "=")
		if !startedCommand && arg == "-" {
			startedCommand = true
		} else if startedCommand || !found {
			startedCommand = true
			cmdArgs = append(cmdArgs, arg)
		} else {
			if err := os.Setenv(name, val); err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
		}
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "error: No command\n")
		os.Exit(1)
	}

	cmd := exec.Command(cmdArgs[0])
	cmd.Args = cmdArgs
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
