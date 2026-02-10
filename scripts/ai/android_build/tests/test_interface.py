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

import unittest
import subprocess
import json
import os
from pathlib import Path
import importlib.resources
from contextlib import ExitStack
import typing

# Add the SDK root to sys.path for absolute imports
sdk_root = Path(__file__).resolve().parent.parent
if not sdk_root.is_dir():
    sdk_root = sdk_root.parent

class TestInterface(unittest.TestCase):
    _cli_path = None
    _exit_stack = None

    @classmethod
    def setUpClass(cls) -> None:
        cls._exit_stack = ExitStack()
        try:
            anchor = importlib.resources.files('tests')
            ref = typing.cast(Path, anchor).parent.joinpath('build_sdk_bin')
            cls._cli_path = str(cls._exit_stack.enter_context(importlib.resources.as_file(ref)))
            os.chmod(cls._cli_path, 0o755)
        except Exception:
            cls._exit_stack.close()
            raise

    @classmethod
    def tearDownClass(cls) -> None:
        if cls._exit_stack:
            cls._exit_stack.close()

    def setUp(self) -> None:
        self.settings_json_path = sdk_root / "settings.json"
        if self.settings_json_path.exists():
            os.remove(self.settings_json_path)

    def _get_cli_path(self) -> str:
        """
        Returns the path to the extracted CLI binary.
        """
        if self._cli_path:
            return self._cli_path

        raise FileNotFoundError("Required binary 'build_sdk_bin' not found or failed to extract. "
                                "Ensure it is built and included as a data dependency.")

    def test_generator_contract(self) -> None:
        """Verify cli.py --generate-mcp-config creates settings.json with placeholders."""
        cli_path = self._get_cli_path()

        # Run generator via CLI
        env = os.environ.copy()
        env["PYTHONPATH"] = str(sdk_root)

        # Append arguments
        full_cmd = [cli_path, "--generate-mcp-config"]

        subprocess.run(full_cmd, cwd=sdk_root, check=True, capture_output=True, env=env)

        self.assertTrue(self.settings_json_path.exists(), "settings.json should exist after running generator")

        with open(self.settings_json_path, "r") as f:
            config = json.load(f)

        self.assertIn("mcpServers", config)
        self.assertIn("android_build", config["mcpServers"])
        server = config["mcpServers"]["android_build"]
        self.assertIn("args", server)

        # KEY CONTRACT: The placeholder must be present literally
        args_str = str(server["args"])
        self.assertIn("${ANDROID_BUILD_TOP}", args_str)

        # Also check env
        env_str = str(server.get("env", {}))
        self.assertIn("${ANDROID_BUILD_TOP}", env_str)

    def test_resolver_contract(self) -> None:
        """Verify cli.py --print-registration-cmd resolves placeholders."""
        # First ensure settings.json exists (using the generator logic)
        cli = self._get_cli_path()
        env = os.environ.copy()
        env["PYTHONPATH"] = str(sdk_root)

        subprocess.run([cli, "--generate-mcp-config"], cwd=sdk_root, check=True, capture_output=True, env=env)

        # Run CLI resolver
        result = subprocess.run(
            [cli, "--print-registration-cmd"],
            cwd=sdk_root,
            check=True,
            capture_output=True,
            text=True,
            env=env
        )

        output = result.stdout.strip()

        # Assert format
        self.assertTrue(output.startswith("gemini mcp add android_build"), "Command should start with gemini mcp add")

        # Assert resolution
        self.assertNotIn("${ANDROID_BUILD_TOP}", output, "Placeholder should be resolved")
        self.assertIn("interface/cli.py", output)
        self.assertIn("-e PYTHONPATH=", output)

if __name__ == "__main__":
    unittest.main()
