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

import dataclasses
import json
from dataclasses import field
from typing import Any, Callable, Optional
from api.env import BuildContext
from api import env, build, ninja, config, module
from .schema import ToolArgs
from .registry import register_tool
from .errors import ToolError

# ... (Args definitions remain same) ...
@dataclasses.dataclass(frozen=True, kw_only=True)
class CommonArgs(ToolArgs):
    product: str = field(metadata={"description": "Target product configuration (e.g., 'aosp_cf_x86_64_only_phone'). This is NOT a Ninja target."})
    release: str = field(metadata={"description": "Target release configuration (e.g., 'trunk_staging')"})
    variant: str = field(metadata={"description": "Target build variant (e.g., 'userdebug')"})
    env_vars: Optional[dict[str, str]] = field(default=None, metadata={"description": "Optional environment variable overrides (e.g., {'EMMA_INSTRUMENT': 'true'})."})
    confirm_analysis: bool = field(default=False, metadata={"description": "Set to True to confirm re-analysis (if env differs) or stale read."})

@dataclasses.dataclass(frozen=True, kw_only=True)
class BuildArgs(CommonArgs):
    targets: list[str]
    clean: bool = False

@dataclasses.dataclass(frozen=True, kw_only=True)
class NinjaQueryArgs(CommonArgs):
    target: str

@dataclasses.dataclass(frozen=True, kw_only=True)
class CheckDependencyArgs(CommonArgs):
    source: str
    target: str

@dataclasses.dataclass(frozen=True, kw_only=True)
class GetCommandArgs(CommonArgs):
    target: str
    last_n: int = 1

@dataclasses.dataclass(frozen=True, kw_only=True)
class AconfigArgs(CommonArgs):
    package: str
    flag: str

@dataclasses.dataclass(frozen=True, kw_only=True)
class ModuleInfoArgs(CommonArgs):
    module_name: str
    force_refresh: bool = False

@dataclasses.dataclass(frozen=True, kw_only=True)
class BuildVarsArgs(CommonArgs):
    vars: list[str]

def raise_reanalysis_error(build_failure: build.BuildFailure) -> None:
    msg = "Reanalysis will run due to changes in e.g. build graph, environment variables, or product configuration."
    for m in str.splitlines(build_failure.message):
        if "Reanalysis will run due to" in m:
            msg = m
    raise ToolError(f"Configuration change detected: {msg}\nRerun the tool with 'confirm_analysis=True' if this is intended.")

@register_tool("build", BuildArgs, wrapped_func=build.build_targets)
def run_build(ctx: BuildContext, args: BuildArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for build.build_targets."""
    # Build already triggers analysis unless confirm_analysis is False.
    # If confirm_analysis is False, we tell build to fail on reanalysis instead of running it.
    result = build.build_targets(ctx, args.targets, args.clean, enforce_no_reanalysis=(not args.confirm_analysis), progress_callback=progress_callback)
    if not result.success:
        failures = result.failure_details
        if not args.confirm_analysis and failures and "Reanalysis will run due to" in failures[0].message:
            raise_reanalysis_error(failures[0])
        else:
             # Return result for structured reporting before error
             # Actual build error
             failure = failures[0] if failures else None
             msg = f"Build Failed: {failure.message}" if failure else "Build Failed"
             if failure and failure.target:
                 msg += f" (Target: {failure.target})"
             raise ToolError(msg)

    return dataclasses.asdict(result)

def _check_env_consistency(ctx: BuildContext, confirm_analysis: bool) -> None:
     if not confirm_analysis:
        check_result = build.build_targets(ctx, ["nothing"], enforce_no_reanalysis=True)
        if not check_result.success:
            failures = check_result.failure_details
            if failures:
                raise_reanalysis_error(failures[0])
            else:
                raise ToolError("Build failed during environment consistency check.")

@register_tool("ninja_query", NinjaQueryArgs, wrapped_func=ninja.query_ninja_target)
def run_ninja_query(ctx: BuildContext, args: NinjaQueryArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for ninja.query_ninja_target."""
    _check_env_consistency(ctx, args.confirm_analysis)

    result = ninja.query_ninja_target(ctx, args.target)
    output = {
        "rule_name": result.rule_name,
        "explicit_deps": result.explicit_deps,
        "implicit_deps": result.implicit_deps,
        "outputs": result.outputs
    }
    return output

@register_tool("check_dependency", CheckDependencyArgs, wrapped_func=ninja.depends_on)
def run_check_dependency(ctx: BuildContext, args: CheckDependencyArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for ninja.depends_on."""
    _check_env_consistency(ctx, args.confirm_analysis)
    is_dep, chain = ninja.depends_on(ctx, args.source, args.target)
    output = {
        "is_dependency": is_dep,
        "dependency_chain": chain
    }
    return output

@register_tool("get_command", GetCommandArgs, wrapped_func=ninja.get_command)
def run_get_command(ctx: BuildContext, args: GetCommandArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for ninja.get_command."""
    _check_env_consistency(ctx, args.confirm_analysis)
    commands = ninja.get_command(ctx, args.target, args.last_n)
    output = {
        "target": args.target,
        "commands": commands
    }
    return output

@register_tool("aconfig", AconfigArgs, wrapped_func=config.get_aconfig_flag)
def run_aconfig(ctx: BuildContext, args: AconfigArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for config.get_aconfig_flag."""
    _check_env_consistency(ctx, args.confirm_analysis)
    flag_info = config.get_aconfig_flag(ctx, args.package, args.flag)
    return flag_info.to_dict()

@register_tool("module_info", ModuleInfoArgs, wrapped_func=module.get_module_info)
def run_module_info(ctx: BuildContext, args: ModuleInfoArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for module.get_module_info."""
    _check_env_consistency(ctx, args.confirm_analysis)
    info = module.get_module_info(ctx, args.module_name, args.force_refresh, progress_callback=progress_callback)
    # Convert ModuleInfo dataclass to dict for JSON serialization
    info_dict = dataclasses.asdict(info)
    return info_dict

@register_tool("build_vars", BuildVarsArgs, wrapped_func=config.get_build_vars)
def run_build_vars(ctx: BuildContext, args: BuildVarsArgs, progress_callback: Optional[Callable[[float, Optional[float]], None]] = None) -> dict[str, Any]:
    """Wrapper for config.get_build_vars."""
    _check_env_consistency(ctx, args.confirm_analysis)
    vars_dict = config.get_build_vars(ctx, *args.vars)
    return vars_dict
