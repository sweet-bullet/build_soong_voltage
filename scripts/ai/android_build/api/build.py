# Copyright (C) 2026 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import subprocess
import re
import dataclasses
from typing import Any, Callable, Optional
from pathlib import Path
from .env import BuildContext
from .constants import SOONG_UI_BASH
from interface.errors import ToolError

class BuildError(ToolError):
    """Custom exception for build failures."""
    def __init__(self, message: str, exit_code: int, logs: str) -> None:
        super().__init__(message)
        self.exit_code = exit_code
        self.logs = logs

@dataclasses.dataclass(frozen=True)
class BuildFailure:
    """Represents a specific failure in the build."""
    message: str
    target: Optional[str] = None
    command: Optional[str] = None
    outputs: Optional[str] = None

@dataclasses.dataclass(frozen=True)
class BuildResult:
    """Represents the outcome of a build."""
    success: bool
    exit_code: int
    failure_details: Optional[list[BuildFailure]] = None

def strip_ansi_codes(text: str) -> str:
    """Removes ANSI escape sequences from a string."""
    ansi_escape = re.compile(r'\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])')
    return ansi_escape.sub('', text)

def parse_build_log(raw_log: str) -> list[BuildFailure]:
    """
    Parses the Android error.log content into a structured list of failures.
    Returns a list of BuildFailure objects.
    """
    # Remove ANSI codes to ensure clean regex matching
    clean_log = strip_ansi_codes(raw_log)

    # Regex to extract structured Soong errors
    # Format is defined in build/soong/ui/status/log.go
    soong_pattern = re.compile(
        r"FAILED:\s+(?P<target>.*)\n"
        r"(?:Outputs:\s+(?P<outputs>.*)\n)?"
        r"(?:Error:\s+(?P<error_summary>.*)\n)?"
        r"(?:Command:\s+(?P<command>.*)\n)?"
        r"Output:\n(?P<output>[\s\S]*?)(?=\n\nFAILED:|$)"
    )

    matches = list(soong_pattern.finditer(clean_log))

    if matches:
        # Scenario 1: Structured Log Found
        structured_failures = []
        for m in matches:
            data = m.groupdict()

            failure = BuildFailure(
                target=(data.get("target") or "").strip(),
                command=(data.get("command") or "").strip(),
                outputs=(data.get("outputs") or "").strip(),
                message=(data.get("output") or "").strip() or "Build failed."
            )
            structured_failures.append(failure)
        return structured_failures
    else:
        # Scenario 2: Unstructured / Ninja Error
        return [BuildFailure(message=clean_log.strip())]

def _execute_build_command(ctx: BuildContext, targets: list[str], enforce_no_reanalysis: bool = False, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> int:
    """Helper to run a single Soong build command."""
    env = ctx.env
    android_build_top = env.get('ANDROID_BUILD_TOP')
    if not android_build_top:
        raise BuildError("ANDROID_BUILD_TOP not found in environment.", 1, "")

    soong_ui_path = Path(android_build_top) / SOONG_UI_BASH
    command = [str(soong_ui_path), "--make-mode"]
    if enforce_no_reanalysis:
        command.append("--enforce-no-reanalysis")
    command.extend(targets)

    process = subprocess.Popen(
        command,
        env=env,
        cwd=android_build_top,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        encoding='utf-8',
        text=True
    )

    # Progress regex: [ 10% 100/1000] ... or [ 8% 14/172 3s remaining] ...
    # Groups: 1=percent, 2=current, 3=total
    progress_re = re.compile(r"^\[\s*(\d+)%\s+(\d+)/(\d+)(?:.*)?\]")

    if process.stdout:
        for line in iter(process.stdout.readline, ''):
            if progress_callback:
                match = progress_re.match(line)
                if match:
                    current = float(match.group(2))
                    total = float(match.group(3))
                    progress_callback(current, total)

    if process.stderr:
        for line in iter(process.stderr.readline, ''):
            pass

    return process.wait()

def _create_failed_result(ctx: BuildContext, exit_code: int) -> BuildResult:
    """Helper to parse logs and create a failure result."""
    env = ctx.env
    android_build_top = env.get('ANDROID_BUILD_TOP', "")
    out_dir = env.get("OUT_DIR", "out")
    error_log_path = Path(android_build_top) / out_dir / "error.log"

    # Default fallback if no log exists
    failure_details: list[BuildFailure] = [BuildFailure(message="No error.log found.")]

    if error_log_path.exists():
        with open(error_log_path, "r") as f:
            failure_details = parse_build_log("".join(f.readlines()))

    return BuildResult(success=False, exit_code=exit_code, failure_details=failure_details)

def build_targets(ctx: BuildContext, targets: list[str], clean: bool = False, enforce_no_reanalysis: bool = False, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> BuildResult:
    """
    Executes an Android build for the given targets.

    Use this to compile code, generate artifacts, or run phony targets like 'nothing' or 'module-info'.
    Returns a BuildResult object with success status and failure details if applicable.

    Args:
        ctx: Evaluated build configuration.
        targets: List of build targets (e.g., 'SystemUI', 'nothing'). These can be module names or Ninja targets.
        clean: If True, runs 'installclean' before building.
        enforce_no_reanalysis: If True, blocks re-running analysis and throws an error if reanalysis is required.
        progress_callback: Optional callback for progress reporting.
    """
    # Step 1: Installclean if requested
    if clean:
        exit_code = _execute_build_command(ctx, ["installclean"], enforce_no_reanalysis=False, progress_callback=None)
        if exit_code != 0:
            return _create_failed_result(ctx, exit_code)

    # Step 2: Main Build
    exit_code = _execute_build_command(ctx, targets, enforce_no_reanalysis=enforce_no_reanalysis, progress_callback=progress_callback)

    if exit_code == 0:
        return BuildResult(success=True, exit_code=0)

    return _create_failed_result(ctx, exit_code)

