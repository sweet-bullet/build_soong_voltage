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
import dataclasses
from pathlib import Path
import sys
from typing import Any
from .constants import SOONG_UI_BASH, ACONFIG_BIN_PATH, ALL_ACONFIG_DECLARATIONS_PB_PATH, ALL_ACONFIG_DECLARATIONS_TARGET
from .env import BuildContext
from .build import build_targets
from interface.errors import ToolError

class MissingAconfigCacheError(ToolError):
    """Custom exception for when aconfig dependencies are missing."""
    pass

def get_build_vars(ctx: BuildContext, *vars: str) -> dict[str, str]:
    """
    Retrieves the values of one or more product variables (e.g., TARGET_ARCH, OUT_DIR).

    Use this to inspect variables like OUT_DIR, TARGET_ARCH, and TRUNK STABLE build flags
    (typically starting with 'RELEASE_'). For aconfig flags, use the 'aconfig' tool.
    Returns a dictionary mapping variable names to values.

    Args:
        vars: List of build variables to retrieve (e.g. 'OUT_DIR', 'RELEASE_VERSION').
    """
    if not vars:
        return {}

    env = ctx.env
    android_build_top = env.get('ANDROID_BUILD_TOP')
    if not android_build_top:
        raise ToolError("ANDROID_BUILD_TOP not found in environment.")

    soong_ui_path = Path(android_build_top) / SOONG_UI_BASH

    # Use --dumpvars-mode with --vars="VAR1 VAR2 ..."
    vars_arg = " ".join(vars)
    command = [str(soong_ui_path), "--dumpvars-mode", f"--vars={vars_arg}"]

    process = subprocess.run(
        command,
        env=env,
        cwd=android_build_top,
        capture_output=True,
        text=True,
        check=True
    )

    # Parse output: VAR='value'
    result: dict[str, str] = {}
    for line in process.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        # Naive parsing for now, assuming simple values (no embedded quotes)
        # Output format is strictly: VAR='value'
        if "=" in line:
            key, value = line.split("=", 1)
            # Remove surrounding single quotes
            if value.startswith("'") and value.endswith("'"):
                value = value[1:-1]
            result[key] = value

    return result

@dataclasses.dataclass(frozen=True)
class AconfigFlag:
    package: str
    name: str
    namespace: str
    description: str
    bug: str
    state: str              # e.g., "ENABLED", "DISABLED"
    permission: str         # e.g., "READ_WRITE", "READ_ONLY"
    is_fixed_read_only: bool
    is_exported: bool
    container: str
    metadata: str

    def to_dict(self) -> dict[str, Any]:
        return dataclasses.asdict(self)

def get_aconfig_flag(ctx: BuildContext, package: str, flag: str) -> AconfigFlag:
    """
    Retrieves the value of an aconfig flag (formatted as <package.name>.<flag_name>).

    Use this ONLY for aconfig flags. For trunk stable build flags (starting with 'RELEASE_'),
    use the 'build_vars' tool instead.
    Note: The state of aconfig flags can change at runtime unless the flag is marked as 'fixed_read_only'.
    Returns an AconfigFlag object containing details like state, permission, and description.

    Args:
        package: The aconfig package name (e.g. 'com.android.systemui').
        flag: The aconfig flag name.
    """
    env = ctx.env
    top = env.get('ANDROID_BUILD_TOP')
    if not top:
        raise ToolError("ANDROID_BUILD_TOP not found in environment.")
    android_build_top = Path(top)
    android_soong_host_out = env.get('ANDROID_SOONG_HOST_OUT')
    if not android_soong_host_out:
        raise ToolError("ANDROID_SOONG_HOST_OUT not found in environment.")
    soong_host_out_abs_path = Path(android_build_top, android_soong_host_out)

    vars_dict = get_build_vars(ctx, "OUT_DIR")
    out_dir_str = vars_dict["OUT_DIR"]
    out_dir_abs_path = Path(android_build_top, out_dir_str)

    env['OUT_DIR'] = str(out_dir_str)

    aconfig_binary_abs_path = soong_host_out_abs_path / ACONFIG_BIN_PATH
    cache_db_abs_path = out_dir_abs_path / ALL_ACONFIG_DECLARATIONS_PB_PATH

    if not aconfig_binary_abs_path.exists() or not cache_db_abs_path.exists():
        print(f"Missing aconfig dependencies, building '{ALL_ACONFIG_DECLARATIONS_TARGET}'...", file=sys.stderr)

        build_targets(ctx, targets=[ALL_ACONFIG_DECLARATIONS_TARGET], clean=False)
        if not aconfig_binary_abs_path.exists() or not cache_db_abs_path.exists():
             raise MissingAconfigCacheError("Failed to build aconfig dependencies.")

    # We use a custom multi-character delimiter to avoid collisions with
    # descriptions that might contain spaces, pipes, or commas.
    delimiter = "^|^"

    # The format string requests every available field
    fmt_string = (
        f"{{package}}{delimiter}"
        f"{{name}}{delimiter}"
        f"{{namespace}}{delimiter}"
        f"{{description}}{delimiter}"
        f"{{bug}}{delimiter}"
        f"{{state}}{delimiter}"
        f"{{permission}}{delimiter}"
        f"{{is_fixed_read_only}}{delimiter}"
        f"{{is_exported}}{delimiter}"
        f"{{container}}{delimiter}"
        f"{{metadata}}"
    )

    full_flag_name = f"{package}.{flag}"
    command = [
        str(aconfig_binary_abs_path),
        "dump-cache",
        "--cache",
        str(cache_db_abs_path),
        f"--filter=fully_qualified_name:{full_flag_name}",
        "--format",
        fmt_string,
    ]

    output = subprocess.run(
        command,
        env=env,
        cwd=android_build_top,
        capture_output=True,
        check=True,
        text=True,
    ).stdout.strip()

    parts = output.split(delimiter)
    if len(parts) != 11:
        raise ToolError(
            f"Unexpected output from aconfig: got {len(parts)} parts, expected 11. "
            f"Raw output: {output}"
        )

    def parse_bool(val: str) -> bool:
        return val.lower() == "true"

    return AconfigFlag(
        package=parts[0],
        name=parts[1],
        namespace=parts[2],
        description=parts[3],
        bug=parts[4],
        state=parts[5],
        permission=parts[6],
        is_fixed_read_only=parse_bool(parts[7]),
        is_exported=parse_bool(parts[8]),
        container=parts[9],
        metadata=parts[10],
    )
