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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenNdkBackedBy(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "backedby.txt")
	libs := []string{"libfoo.so", "libbar.so", "libbaz.so"}

	genNdkBackedBy(outFile, libs)

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	expected := "libfoo.so libbar.so libbaz.so\n"
	if string(content) != expected {
		t.Errorf("Expected %q, got %q", expected, string(content))
	}
}

func TestGenJavaUsedBy(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "java_usedby.xml")

	// Create a mock 'dexdeps' shell script.
	// It simulates success for 'pass.apk' and simulates a crash (exit 1) for 'fail.apk'
	mockDexDeps := filepath.Join(tmpDir, "mock_dexdeps.sh")
	mockScript := `#!/bin/sh
if [ "$1" = "fail.apk" ]; then
	exit 1
fi
echo "com.example.mockclass"
`
	if err := os.WriteFile(mockDexDeps, []byte(mockScript), 0755); err != nil {
		t.Fatalf("Failed to write mock dexdeps: %v", err)
	}

	files := []string{"pass.apk", "fail.apk"}
	genJavaUsedBy(mockDexDeps, outFile, files)

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	actual := string(content)
	// We expect the XML header, the output of the passing APK,
	// the fallback tag for the failing APK, and the XML footer.
	expectedLines := []string{
		"<externals>",
		"com.example.mockclass",
		"</external>",
		"</externals>",
		"",
	}
	expected := strings.Join(expectedLines, "\n")

	if actual != expected {
		t.Errorf("XML generation or fallback logic failed.\nExpected:\n%s\nGot:\n%s", expected, actual)
	}
}

func TestParseReadelfOutput(t *testing.T) {
	tmpDir := t.TempDir()
	inFile := filepath.Join(tmpDir, "readelf_raw.txt")
	outFile := filepath.Join(tmpDir, "ndk_usedby.txt")

	// Simulate the messy raw output of llvm-readelf.
	// We include WEAK symbols, LOCAL symbols, and irrelevant lines to prove the regex filters correctly.
	rawOutput := `
 1: 00000000  0 FUNC GLOBAL DEFAULT UND dlopen@LIBC
 2: 00000000  0 OBJECT GLOBAL DEFAULT UND ignored_variable@LIBC
 3: 00000000  0 FUNC WEAK DEFAULT UND ignored_weak@LIBC
 4: 00000000  0 FUNC GLOBAL DEFAULT UND malloc@LIBC
 5: 00000000  0 NOT_A_FUNC GLOBAL DEFAULT UND ignore_me@LIBC
 6: 00000000  0 FUNC LOCAL DEFAULT UND local_func@LIBC
`
	if err := os.WriteFile(inFile, []byte(rawOutput), 0666); err != nil {
		t.Fatalf("Failed to write mock input: %v", err)
	}

	parseReadelfOutput(inFile, outFile)

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	actual := string(content)
	// Only the GLOBAL FUNC UND symbols should be extracted
	expected := "dlopen@LIBC\nmalloc@LIBC\n\n"

	if actual != expected {
		t.Errorf("Regex parsing failed.\nExpected %q\nGot %q", expected, actual)
	}
}

func TestLookForExecFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Valid ELF header bytes
	elfMagic := []byte("\x7fELF")

	// 1. A standard .so file with an ELF header (should be processed)
	soFile := filepath.Join(tmpDir, "libtest.so")
	os.WriteFile(soFile, elfMagic, 0644)

	// 2. A normal text file (should be IGNORED)
	txtFile := filepath.Join(tmpDir, "readme.txt")
	os.WriteFile(txtFile, []byte("dummy text"), 0644)

	// 3. An executable binary with an ELF header (should be processed)
	binFile := filepath.Join(tmpDir, "my_executable")
	os.WriteFile(binFile, elfMagic, 0755)

	// 4. An executable shell script WITHOUT an ELF header (should be IGNORED)
	// This explicitly tests the fix for the bin/media_provider crash!
	scriptFile := filepath.Join(tmpDir, "media_provider_script")
	os.WriteFile(scriptFile, []byte("#!/bin/sh\necho 'hello'"), 0755)

	// Create a mock readelf script that just echoes the file name it was passed
	mockReadelf := filepath.Join(tmpDir, "mock_readelf.sh")
	mockScript := `#!/bin/sh
echo "Processed: $(basename $2)"
`
	os.WriteFile(mockReadelf, []byte(mockScript), 0755)

	// Setup output capture
	tmpOutputFile := filepath.Join(tmpDir, "output.txt")
	tmpOutputF, _ := os.OpenFile(tmpOutputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)

	// Run the logic
	lookForExecFile(tmpDir, mockReadelf, tmpOutputF)
	tmpOutputF.Close()

	// Verify the correct files were processed
	contentBytes, _ := os.ReadFile(tmpOutputFile)
	actual := string(contentBytes)

	if !strings.Contains(actual, "Processed: libtest.so") {
		t.Errorf("Failed to process standard .so file")
	}
	if !strings.Contains(actual, "Processed: my_executable") {
		t.Errorf("Failed to process executable ELF binary")
	}
	if strings.Contains(actual, "Processed: readme.txt") {
		t.Errorf("Incorrectly processed a non-executable .txt file")
	}
	if strings.Contains(actual, "Processed: media_provider_script") {
		t.Errorf("Regression: Incorrectly processed an executable non-ELF script")
	}
}
