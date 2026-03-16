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
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func printHelp() {
	fmt.Println("**************************** Usage Instructions ****************************")
	fmt.Println("This tool generates API info files for Mainline modules.")
	fmt.Println("")
	fmt.Println("Usage: gen_apex_api_info <command> [args...]")
	fmt.Println("Commands:")
	fmt.Println("  ndk_backedby <output_file> [lib1] [lib2]...")
	fmt.Println("  java_usedby  <dexdeps_path> <output_file> [jar_or_apk1] [jar_or_apk2]...")
	fmt.Println("  ndk_usedby   <image_dir> <readelf_path> <output_file>")
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" {
		printHelp()
		os.Exit(0)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "ndk_backedby":
		if len(args) < 1 {
			fmt.Println("Wrong argument length. Expecting at least 1 argument representing output path, followed by a list of libraries in the Mainline module.")
			os.Exit(1)
		}
		genNdkBackedBy(args[0], args[1:])

	case "java_usedby":
		if len(args) < 2 {
			fmt.Println("Wrong argument length. Expecting at least 2 argument representing dexdeps path, output path, followed by a list of jar or apk files in the Mainline module.")
			os.Exit(1)
		}
		genJavaUsedBy(args[0], args[1], args[2:])

	case "ndk_usedby":
		if len(args) != 4 {
			fmt.Println("Wrong argument length. Expecting 4 arguments: image file directory, llvm-readelf tool path, zipsync path, output path.")
			os.Exit(1)
		}
		genNdkUsedBy(args[0], args[1], args[2], args[3])

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func genNdkBackedBy(outFile string, libs []string) {
	content := strings.Join(libs, " ") + "\n"
	err := os.WriteFile(outFile, []byte(content), 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write %s: %v\n", outFile, err)
		os.Exit(1)
	}
}

func genJavaUsedBy(dexdepsPath, outFile string, files []string) {
	f, err := os.Create(outFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", outFile, err)
		os.Exit(1)
	}
	defer f.Close()

	if _, err := f.WriteString("<externals>\n"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write to %s: %v\n", outFile, err)
		os.Exit(1)
	}

	for _, file := range files {
		cmd := exec.Command(dexdepsPath, file)
		cmd.Stdout = f
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if _, errWrite := f.WriteString("</external>\n"); errWrite != nil {
				fmt.Fprintf(os.Stderr, "Failed to write fallback to %s: %v\n", outFile, errWrite)
				os.Exit(1)
			}
		}
	}

	if _, err := f.WriteString("</externals>\n"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write to %s: %v\n", outFile, err)
		os.Exit(1)
	}
}

func genNdkUsedBy(imageDir, readelfPath, zipsyncPath, outputFile string) {
	tmpReadelfFile, err := os.CreateTemp("", "temporary-file.*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpReadelfOutput := tmpReadelfFile.Name()
	defer tmpReadelfFile.Close()
	defer os.Remove(tmpReadelfOutput)

	tmpUnzippedDir, err := os.MkdirTemp("", "temporary-dir.*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpUnzippedDir)

	// If there are any jars or apks, unzip them to surface native files.
	unzipJarAndApk(imageDir, tmpUnzippedDir, zipsyncPath)
	// Analyze the unzipped files.
	lookForExecFile(tmpUnzippedDir, readelfPath, tmpReadelfFile)
	// Analyze the apex image staging dir itself.
	lookForExecFile(imageDir, readelfPath, tmpReadelfFile)

	tmpReadelfFile.Close()

	os.Remove(outputFile)
	parseReadelfOutput(tmpReadelfOutput, outputFile)
}

func unzipJarAndApk(dir, tmpUnzippedDir, zipsyncPath string) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jar") || strings.HasSuffix(d.Name(), ".apk") {
			// Create a unique subdirectory so zipsync doesn't delete previous extractions
			subDir := filepath.Join(tmpUnzippedDir, d.Name()+"_extracted")

			cmd := exec.Command(zipsyncPath, "-d", subDir, path)
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "zipsync failed: %v\n", err)
				os.Exit(1)
			}
		}
		return nil
	})

	filepath.WalkDir(tmpUnzippedDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".MF") {
			os.Remove(path)
		}
		return nil
	})
}

// isElf checks if a file is a valid ELF binary by reading its 4-byte magic number.
// This prevents llvm-readelf from crashing when the tool encounters plain-text
// wrapper scripts (like bin/media_provider) that have executable permissions.
func isElf(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return false
	}

	return string(magic) == "\x7fELF"
}

func lookForExecFile(dir, readelfPath string, tmpOutput *os.File) {
	filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		realInfo := info
		if info.Mode()&os.ModeSymlink != 0 {
			var statErr error
			realInfo, statErr = os.Stat(path)
			if statErr != nil {
				return nil // Skip broken symlinks
			}
		}

		if !realInfo.Mode().IsRegular() {
			return nil
		}

		isSo := strings.HasSuffix(realInfo.Name(), ".so")
		isExec := realInfo.Mode().Perm()&0111 != 0

		if (isSo || isExec) && isElf(path) {
			cmd := exec.Command(readelfPath, "--dyn-symbols", path)
			cmd.Stdout = tmpOutput
			cmd.Stderr = os.Stderr // preserve errors for debugging
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "readelf failed on %s: %v\n", path, err)
				os.Exit(1)
			}
		}

		return nil
	})
}

var readelfSymbolRegex = regexp.MustCompile(`.*\bFUNC\b.*\bGLOBAL\b.*\bUND\b\s+(.*@.*)`)

func parseReadelfOutput(readelfOutput, ndkApisOutput string) {
	inFile, err := os.Open(readelfOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open %s: %v\n", readelfOutput, err)
		os.Exit(1)
	}
	defer inFile.Close()

	outFile, err := os.Create(ndkApisOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", ndkApisOutput, err)
		os.Exit(1)
	}
	defer outFile.Close()

	scanner := bufio.NewScanner(inFile)
	for scanner.Scan() {
		line := scanner.Text()
		matches := readelfSymbolRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			if _, err := outFile.WriteString(matches[1] + "\n"); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write out: %v\n", err)
				os.Exit(1)
			}
		}
	}
	outFile.WriteString("\n")
}
