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

import json
import dataclasses
from pathlib import Path
from typing import Any
from .env import BuildContext
from .config import get_build_vars
from .build import build_targets
from interface.errors import ToolError

MODULE_INFO_JSON = "module-info.json"

@dataclasses.dataclass(frozen=True)
class ModuleInfo:
    name: str
    classes: list[str]
    paths: list[str]
    installed: list[str]
    compatibility_suites: list[str]
    test_config: list[str]

    @classmethod
    def from_dict(cls, name: str, data: dict[str, Any]) -> "ModuleInfo":
        return cls(
            name=name,
            classes=data.get("class", []),
            paths=data.get("path", []),
            installed=data.get("installed", []),
            compatibility_suites=data.get("compatibility_suites", []),
            test_config=data.get("test_config", [])
        )

# Global Caches
_MODULE_INFO_CACHE: dict[str, ModuleInfo] = {}
_MODULE_INFO_MTIME: float = 0.0

def _get_product_out(ctx: BuildContext) -> Path:
    """Resolves the PRODUCT_OUT directory."""
    env = ctx.env
    android_build_top = env.get("ANDROID_BUILD_TOP")
    if not android_build_top:
        raise ToolError("ANDROID_BUILD_TOP not found in environment.")

    # Try getting it from build vars first (most accurate)
    try:
        vars_dict = get_build_vars(ctx, "PRODUCT_OUT")
        out_dir = vars_dict.get("PRODUCT_OUT")
        if out_dir:
            path = Path(out_dir)
            if not path.is_absolute():
                path = Path(android_build_top) / path
            return path
    except Exception:
        pass

    # Fallback to env var or standard location
    if env.get("ANDROID_PRODUCT_OUT"):
        return Path(str(env.get("ANDROID_PRODUCT_OUT")))

    # Last resort fallback
    product = env.get("TARGET_PRODUCT")
    if not product:
        raise ToolError("TARGET_PRODUCT not found in environment.")
    return Path(android_build_top) / "out/target/product" / product

def _load_json_db(ctx: BuildContext, force_refresh: bool = False) -> dict[str, ModuleInfo]:
    """
    Loads module-info.json, rebuilding or reloading if necessary.
    Returns the dictionary of ModuleInfo objects.
    """
    global _MODULE_INFO_CACHE, _MODULE_INFO_MTIME

    product_out = _get_product_out(ctx)
    module_info_path = product_out / MODULE_INFO_JSON

    # Step 1: Check if file exists. If not, we MUST build.
    if not module_info_path.exists() or force_refresh:
        build_targets(ctx, targets=["module-info"], clean=False)

        if not module_info_path.exists():
            raise ToolError(f"Build failed to generate {module_info_path}")

    # Step 2: Check mtime to see if we need to reload from disk
    current_mtime = module_info_path.stat().st_mtime
    if _MODULE_INFO_CACHE and current_mtime == _MODULE_INFO_MTIME and not force_refresh:
        return _MODULE_INFO_CACHE

    # Step 3: Load from disk
    with open(module_info_path, "r") as f:
        raw_data = json.load(f)

    # Step 4: Parse and Cache
    new_cache = {}
    for name, data in raw_data.items():
        new_cache[name] = ModuleInfo.from_dict(name, data)

    _MODULE_INFO_CACHE = new_cache
    _MODULE_INFO_MTIME = current_mtime

    return _MODULE_INFO_CACHE

def get_module_info(ctx: BuildContext, module_name: str, force_refresh: bool = False) -> ModuleInfo:
    """
    Retrieves information for a specific module.

    Use this to find source paths, installed paths, and class tags for a module.
    Returns a ModuleInfo object containing the module metadata.

    Args:
        module_name: The name of the module to inspect (e.g. 'SystemUI').
        force_refresh: If True, forces a rebuild of module-info.json.
    """
    db = _load_json_db(ctx, force_refresh)
    if module_name not in db:
        raise ToolError(f"Module '{module_name}' not found in module-info.json")
    return db[module_name]
