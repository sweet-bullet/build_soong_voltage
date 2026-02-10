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
from enum import Enum
from pathlib import Path
from .env import BuildContext
from .config import get_build_vars
from .constants import NINJA_BIN_PATH
from interface.errors import ToolError

class NinjaTargetNotFoundError(ToolError):
    """Raised when a Ninja target is not found."""
    pass

@dataclasses.dataclass(frozen=True)
class NinjaQuery:
    """Holds the parsed results of `ninja -t query`."""
    rule_name: str = ""
    explicit_deps: list[str] = dataclasses.field(default_factory=list)
    implicit_deps: list[str] = dataclasses.field(default_factory=list)
    order_only_deps: list[str] = dataclasses.field(default_factory=list)
    validations: list[str] = dataclasses.field(default_factory=list)
    validations_for: list[str] = dataclasses.field(default_factory=list)
    outputs: list[str] = dataclasses.field(default_factory=list)

def _get_ninja_runner(ctx: BuildContext) -> list[str]:
    """
    Returns a prepared subprocess command list prefix for running ninja.
    """
    env = ctx.env
    android_build_top = env.get("ANDROID_BUILD_TOP")
    if not android_build_top:
        raise ToolError("ANDROID_BUILD_TOP not found in environment")

    # Resolve OUT_DIR
    vars_dict = get_build_vars(ctx, "OUT_DIR")
    out_dir = vars_dict.get("OUT_DIR", "out")

    # Resolve TARGET_PRODUCT
    target_product = env.get("TARGET_PRODUCT", ctx.product)

    out_path = Path(out_dir)
    if not out_path.is_absolute():
        out_path = Path(android_build_top) / out_path

    ninja_file = out_path / f"combined-{target_product}.ninja"

    # Resolve Ninja Binary
    ninja_bin = env.get("NINJA")
    if not ninja_bin:
        ninja_bin = str(Path(android_build_top) / NINJA_BIN_PATH)

    return [ninja_bin, "-f", str(ninja_file)]

def query_ninja_target(ctx: BuildContext, target: str) -> NinjaQuery:
    """
    Queries the Ninja graph for a specific target.

    Use this to inspect dependencies (input) and outputs of a specific Ninja graph node (target).
    Returns the explicit and implicit dependencies and output files.

    Args:
        target: The Ninja graph node (target) to query. This is usually a file path or a phony target name.
    """
    cmd = _get_ninja_runner(ctx) + ["-t", "query", target]

    env = ctx.env
    android_build_top = env.get("ANDROID_BUILD_TOP")

    try:
        process = subprocess.run(
            cmd,
            env=env,
            cwd=android_build_top,
            capture_output=True,
            text=True,
            check=True
        )
    except subprocess.CalledProcessError as e:
        if "unknown target" in e.stderr:
            raise NinjaTargetNotFoundError(f"Target '{target}' not found: {e.stderr.strip()}")
        raise ToolError(f"Ninja query failed: {e.stderr.strip()}")

    class NinjaQuerySection(Enum):
        INPUT = 1
        OUTPUTS = 2
        VALIDATIONS = 3
        VALIDATION_FOR = 4

    rule_name = ""
    explicit_deps = []
    implicit_deps = []
    order_only_deps = []
    validations = []
    validations_for = []
    outputs = []

    # Parsing logic
    current_section = None

    for line in process.stdout.splitlines():
        line = line.rstrip() # keep leading spaces for indentation check
        if not line:
            continue

        # The target line itself (e.g. "out/target/...:")
        if line.strip().startswith(f"{target}:"):
            continue

        stripped = line.strip()

        if stripped.startswith("input: "):
            current_section = NinjaQuerySection.INPUT
            rule_name = stripped.removeprefix("input: ").strip()
            continue
        elif stripped == "outputs:":
            current_section = NinjaQuerySection.OUTPUTS
            continue
        elif stripped == "validations:":
            current_section = NinjaQuerySection.VALIDATIONS
            continue
        elif stripped == "validation for:":
            current_section = NinjaQuerySection.VALIDATION_FOR
            continue

        match current_section:
            case NinjaQuerySection.INPUT:
                # Check for subsections within input
                # Ninja query output looks like:
                # target:
                #   input: <target_name>
                #     explicit_dep
                #     | implicit_dep
                #     || order_only_dep
                # Implicit deps start with "| "
                # Order-only deps start with "|| "

                if stripped.startswith("| "):
                    implicit_deps.append(stripped.removeprefix("| ").strip())
                elif stripped.startswith("|| "):
                    order_only_deps.append(stripped.removeprefix("|| ").strip())
                else:
                    explicit_deps.append(stripped)
            case NinjaQuerySection.OUTPUTS:
                outputs.append(stripped)
            case NinjaQuerySection.VALIDATIONS:
                validations.append(stripped)
            case NinjaQuerySection.VALIDATION_FOR:
                validations_for.append(stripped)

    return NinjaQuery(
        rule_name=rule_name,
        explicit_deps=explicit_deps,
        implicit_deps=implicit_deps,
        order_only_deps=order_only_deps,
        validations=validations,
        validations_for=validations_for,
        outputs=outputs
    )

def depends_on(ctx: BuildContext, source: str, target: str) -> tuple[bool, list[str]]:
    """
    Checks if `target` (the consumer) depends on `source` (the input).

    Use this to verify if one file or target depends on another within the build graph.
    Returns a boolean (True if path exists) and the dependency chain if found.

    Args:
        source: The source Ninja target (potential dependency).
        target: The destination Ninja target (potential dependent).
    """
    # ninja -t path <output_target> <input_target>
    # target is the output/consumer. source is the input/dependency.
    cmd = _get_ninja_runner(ctx) + ["-t", "path", target, source]

    env = ctx.env
    android_build_top = env.get("ANDROID_BUILD_TOP")

    try:
        process = subprocess.run(
            cmd,
            env=env,
            cwd=android_build_top,
            capture_output=True,
            text=True,
            check=True
        )
        # If successful, a path exists
        chain = [line.strip() for line in process.stdout.splitlines() if line.strip()]
        return True, chain

    except subprocess.CalledProcessError:
        # Ninja returns non-zero if no path found or target missing
        return False, []

def get_command(ctx: BuildContext, target: str, last_n: int = 1) -> list[str]:
    """
    Retrieves the build command(s) for a given target.

    Use this to inspect the exact compiler flags, search paths, and tools used to generate an artifact.
    Returns a list of the last 'last_n' commands in the sequence required to build the target.

    Args:
        target: The Ninja target whose build command should be retrieved.
        last_n: The number of last commands to retrieve from the sequence (default: 1).
    """
    cmd = _get_ninja_runner(ctx) + ["-t", "commands", target]

    env = ctx.env
    android_build_top = env.get("ANDROID_BUILD_TOP")

    try:
        process = subprocess.run(
            cmd,
            env=env,
            cwd=android_build_top,
            capture_output=True,
            text=True,
            check=True
        )
        lines = [line.strip() for line in process.stdout.splitlines() if line.strip()]
        return lines[-last_n:]

    except subprocess.CalledProcessError as e:
        raise ToolError(f"Ninja commands query failed: {e.stderr.strip()}")
