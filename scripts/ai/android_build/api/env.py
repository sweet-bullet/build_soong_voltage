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
import subprocess
from pathlib import Path
from typing import Optional
from interface.errors import ToolError
from .constants import ENVSETUP_PATH

class BuildContext:
    def __init__(self, product: str, release: str, variant: str, env_overrides: Optional[dict[str, str]] = None) -> None:
        """Holds build context information."""
        self.product = product
        self.release = release
        self.variant = variant
        self.env_overrides = env_overrides or {}

        # Validate that restricted keys are not in env_overrides
        restricted_keys = {"TARGET_PRODUCT", "TARGET_RELEASE", "TARGET_BUILD_VARIANT"}
        if self.env_overrides:
            found_restricted = restricted_keys.intersection(self.env_overrides.keys())
            if found_restricted:
                raise ToolError(
                    f"The following environment variables are reserved and cannot be passed in env_vars: {', '.join(sorted(found_restricted))}. "
                    "Please use the top-level arguments (product, release, variant) instead."
                )

        self._env_snapshot = EnvSnapshot(product, release, variant)

    @property
    def env(self) -> dict[str, str]:
        """Lazily retrieves the environment."""
        base_env = self._env_snapshot.get_env()
        if self.env_overrides:
            # Return a new dict with overrides applied
            return {**base_env, **self.env_overrides}
        return base_env

class EnvSnapshot:
    """Handles creating and loading environment caches."""
    def __init__(self, product: str, release: str, variant: str) -> None:
        self.product = product
        self.release = release
        self.variant = variant

        # The script is in api/, so the sdk root is one level up
        sdk_root = Path(__file__).parent.parent
        self.cache_dir = sdk_root / ".cache"
        self.cache_file = self.cache_dir / f"env_{product}_{release}_{variant}.json"

        # The repo root is 6 levels up from the script
        self.repo_root = sdk_root.parent.parent.parent.parent.parent

    @property
    def envsetup_path(self) -> Path:
        return self.repo_root / ENVSETUP_PATH

    def is_cache_fresh(self) -> bool:
        """Checks if the cache file exists and is newer than envsetup.sh."""
        if not self.cache_file.exists():
            return False

        cache_mtime = self.cache_file.stat().st_mtime
        envsetup_mtime = self.envsetup_path.stat().st_mtime

        return cache_mtime > envsetup_mtime

    def load(self) -> Optional[dict[str, str]]:
        """Loads the environment from the cache file."""
        if not self.cache_file.exists():
            return None

        with open(self.cache_file, "r") as f:
            return json.load(f) # type: ignore

    def refresh(self) -> dict[str, str]:
        """Refreshes the environment by running the lunch command."""
        lunch_target = f"{self.product}-{self.release}-{self.variant}"

        # This python script will be executed in the bash subshell
        python_script = "import os, json; print(json.dumps(dict(os.environ)))"

        command = f"bash -c 'source {self.envsetup_path} >/dev/null && lunch {lunch_target} >/dev/null && python3 -c \"{python_script}\"'"

        process = subprocess.Popen(
            command,
            shell=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            cwd=self.repo_root,
            encoding='utf-8'
        )

        stdout, stderr = process.communicate()

        if process.returncode != 0:
            raise ToolError(f"Failed to refresh environment for {lunch_target}:\n{stderr}")

        env: dict[str, str] = json.loads(stdout)
        self.save(env)
        return env

    def save(self, env: dict[str, str]) -> None:
        """Saves the environment to the cache file."""
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        with open(self.cache_file, "w") as f:
            json.dump(env, f)

    def get_env(self, force_refresh: bool = False) -> dict[str, str]:
        """
        Gets the build environment for a given EnvSnapshot.

        Args:
            force_refresh: If True, forces a refresh of the environment cache.

        Returns:
            A dictionary representing the environment.
        """

        if not force_refresh and self.is_cache_fresh():
            env = self.load()
            if env:
                return env

        return self.refresh()
