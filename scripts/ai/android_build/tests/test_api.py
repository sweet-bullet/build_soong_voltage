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
import tempfile
import shutil
import json
import io
import os
from pathlib import Path
from typing import Optional, Any
from unittest.mock import patch, MagicMock, PropertyMock
from api.env import BuildContext
from api.build import build_targets, parse_build_log, BuildError
from api.config import get_build_vars, get_aconfig_flag
from api.ninja import query_ninja_target, depends_on, get_command
from api.module import get_module_info

# Constants for testing
TEST_PRODUCT = "aosp_cf_x86_64_only_phone"
TEST_RELEASE = "trunk_staging"
TEST_VARIANT = "userdebug"

class TestApi(unittest.TestCase):
    _test_root: str
    ctx: BuildContext

    @classmethod
    def setUpClass(cls) -> None:
        """Setup a fake build environment."""
        cls._test_root = tempfile.mkdtemp(prefix="android_build_test_")

        # Initialize Context with OUT_DIR override
        cls.ctx = BuildContext(
            TEST_PRODUCT,
            TEST_RELEASE,
            TEST_VARIANT,
            env_overrides={"OUT_DIR": "out"}
        )

        # Create base directories
        out_dir = Path(cls._test_root) / "out"
        out_dir.mkdir(parents=True, exist_ok=True)

        # Create fake module-info.json
        module_info = {
            "SystemUI": {
                "class": ["APPS"],
                "path": ["frameworks/base/packages/SystemUI"],
                "installed": [str(out_dir / f"{TEST_PRODUCT}/system/priv-app/SystemUI/SystemUI.apk")],
                "compatibility_suites": [],
                "test_config": []
            }
        }
        target_product_out = out_dir / "target/product" / TEST_PRODUCT
        target_product_out.mkdir(parents=True, exist_ok=True)
        with open(target_product_out / "module-info.json", "w") as f:
            json.dump(module_info, f)

        # Create fake combined-*.ninja
        ninja_content = """
build out/target/product/test/system/priv-app/SystemUI/SystemUI.apk: android_app frameworks/base/packages/SystemUI/AndroidManifest.xml
  validation: out/target/product/test/system/priv-app/SystemUI/SystemUI.apk.vdex

build nothing: phony
"""
        with open(out_dir / f"combined-{TEST_PRODUCT}.ninja", "w") as f:
            f.write(ninja_content)

    @classmethod
    def tearDownClass(cls) -> None:
        """Cleanup the temp dir."""
        if hasattr(cls, "_test_root"):
            shutil.rmtree(cls._test_root)

    def setUp(self) -> None:
        # Mock BuildContext.env globally
        self.env_patcher = patch.object(BuildContext, "env", new_callable=PropertyMock)
        self.mock_env = self.env_patcher.start()

        # Ensure paths are absolute and consistent
        root = str(Path(self._test_root).resolve())
        self.mock_env.return_value = {
            "ANDROID_BUILD_TOP": root,
            "OUT_DIR": "out",
            "TARGET_PRODUCT": TEST_PRODUCT,
            "ANDROID_PRODUCT_OUT": str(Path(root) / "out/target/product" / TEST_PRODUCT),
            "ANDROID_SOONG_HOST_OUT": "out/host/linux-x86"
        }

    def tearDown(self) -> None:
        self.env_patcher.stop()

    @patch("subprocess.Popen")
    def test_build_targets_success(self, mock_popen: MagicMock) -> None:
        mock_proc = MagicMock()
        mock_proc.returncode = 0
        mock_proc.wait.return_value = 0
        mock_proc.stdout = io.StringIO("[ 50% 1/2] Building...\n[100% 2/2] Done\n")
        mock_proc.stderr = io.StringIO("")
        mock_popen.return_value = mock_proc

        progress_updates = []
        def cb(c: float, t: Optional[float]) -> None:
            progress_updates.append((c, t))

        result = build_targets(self.ctx, ["nothing"], progress_callback=cb)

        self.assertTrue(result.success)
        self.assertEqual(len(progress_updates), 2)

    @patch("subprocess.Popen")
    def test_build_targets_clean(self, mock_popen: MagicMock) -> None:
        mock_proc = MagicMock()
        mock_proc.returncode = 0
        mock_proc.wait.return_value = 0
        mock_proc.stdout = io.StringIO("")
        mock_proc.stderr = io.StringIO("")

        mock_popen.side_effect = [mock_proc, mock_proc]

        result = build_targets(self.ctx, ["nothing"], clean=True)

        self.assertTrue(result.success)
        self.assertEqual(mock_popen.call_count, 2)

    @patch("subprocess.Popen")
    def test_build_targets_failure(self, mock_popen: MagicMock) -> None:
        mock_proc = MagicMock()
        mock_proc.returncode = 1
        mock_proc.wait.return_value = 1
        mock_proc.stdout = io.StringIO("")
        mock_proc.stderr = io.StringIO("")
        mock_popen.return_value = mock_proc

        # Create error.log in the mocked location
        root = self.mock_env.return_value["ANDROID_BUILD_TOP"]
        error_log_path = Path(root) / "out" / "error.log"
        error_log_path.parent.mkdir(parents=True, exist_ok=True)
        error_log_path.write_text("FAILED: target\nError: something broke\nOutput:\ncompiler error")

        result = build_targets(self.ctx, ["nothing"])

        self.assertFalse(result.success)
        self.assertEqual(result.exit_code, 1)
        self.assertIsNotNone(result.failure_details)
        self.assertEqual(result.failure_details[0].message, "compiler error") # type: ignore

    def test_parse_build_log_structured(self) -> None:
        raw_log = "FAILED: target\nError: compilation failed\nOutput:\nerror at line 1"
        result = parse_build_log(raw_log)
        self.assertEqual(result[0].message, "error at line 1")

    def test_parse_build_log_unstructured(self) -> None:
        raw_log = "generic error"
        result = parse_build_log(raw_log)
        self.assertEqual(result[0].message, raw_log)

    def test_parse_build_log_ansi(self) -> None:
        # Siso log with ANSI codes and multiple lines of output
        raw_log = (
            "\x1b[31;1mFAILED: //.:target\x1b[0m\n"
            "Output:\n"
            "some_file.cpp:1:5: \x1b[31merror:\x1b[0m use of undeclared identifier 'x'\n"
            "    1 | int x = y;\n"
            "      |         ^\n"
            "\n"
            "\x1b[31;1mFAILED: //.:next_target\x1b[0m\n"
        )
        result = parse_build_log(raw_log)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].target, "//.:target")
        self.assertIn("use of undeclared identifier 'x'", result[0].message)
        self.assertIn("int x = y;", result[0].message)
        # Ensure ANSI codes are stripped from fields
        self.assertNotIn("\x1b[", result[0].target or "")
        self.assertNotIn("\x1b[", result[0].message or "")

    @patch("subprocess.run")
    def test_get_build_vars(self, mock_run: MagicMock) -> None:
        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "VAR1='val1'\nVAR2='val2'"
        mock_run.return_value = mock_result

        vars_dict = get_build_vars(self.ctx, "VAR1", "VAR2")
        self.assertEqual(vars_dict.get("VAR1"), "val1")

    @patch("subprocess.run")
    def test_aconfig(self, mock_run: MagicMock) -> None:
        delim = "^|^"
        output = delim.join(["pkg", "name", "ns", "desc", "bug", "ENABLED", "RW", "false", "true", "sys", "{}"])

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = output # Now a string because of text=True

        mock_out_dir = MagicMock()
        mock_out_dir.returncode = 0
        mock_out_dir.stdout = "OUT_DIR='out'"

        mock_run.side_effect = [mock_out_dir, mock_result]

        # Create dummy files
        root = self.mock_env.return_value["ANDROID_BUILD_TOP"]
        aconfig_path = Path(root) / "out/host/linux-x86/bin/aconfig"
        aconfig_path.parent.mkdir(parents=True, exist_ok=True)
        aconfig_path.touch()

        db_path = Path(root) / "out/soong/.intermediates/all_aconfig_declarations.pb"
        db_path.parent.mkdir(parents=True, exist_ok=True)
        db_path.touch()

        with patch("api.config.build_targets") as mock_build:
            flag = get_aconfig_flag(self.ctx, "pkg", "name")

        self.assertEqual(flag.state, "ENABLED")

    @patch("subprocess.run")
    def test_ninja_query(self, mock_run: MagicMock) -> None:
        mock_out_dir = MagicMock()
        mock_out_dir.returncode = 0
        mock_out_dir.stdout = "OUT_DIR='out'"

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "target:\n  input: rule\n    dep\n  outputs:\n    out"

        mock_run.side_effect = [mock_out_dir, mock_result]

        q = query_ninja_target(self.ctx, "target")
        self.assertIn("dep", q.explicit_deps)

    @patch("subprocess.run")
    def test_check_dependency(self, mock_run: MagicMock) -> None:
        mock_out_dir = MagicMock()
        mock_out_dir.returncode = 0
        mock_out_dir.stdout = "OUT_DIR='out'"

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "target\nsource"

        mock_run.side_effect = [mock_out_dir, mock_result]

        is_dep, chain = depends_on(self.ctx, "source", "target")
        self.assertTrue(is_dep)

    @patch("subprocess.run")
    def test_get_command(self, mock_run: MagicMock) -> None:
        mock_out_dir = MagicMock()
        mock_out_dir.returncode = 0
        mock_out_dir.stdout = "OUT_DIR='out'"

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "command"

        mock_run.side_effect = [mock_out_dir, mock_result, mock_out_dir, mock_result]

        cmds = get_command(self.ctx, "target")
        self.assertEqual(cmds, ["command"])

    @patch("api.module.get_build_vars")
    def test_module_info(self, mock_get_build_vars: MagicMock) -> None:
        root = self.mock_env.return_value["ANDROID_BUILD_TOP"]
        mock_get_build_vars.return_value = {"PRODUCT_OUT": str(Path(root) / "out/target/product" / TEST_PRODUCT)}

        mod_info = get_module_info(self.ctx, "SystemUI")
        self.assertEqual(mod_info.name, "SystemUI")

if __name__ == "__main__":
    unittest.main()
