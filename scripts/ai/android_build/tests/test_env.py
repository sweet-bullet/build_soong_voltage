import unittest
import json
from unittest.mock import MagicMock, patch, mock_open
from pathlib import Path
from api.env import BuildContext, EnvSnapshot
from interface.errors import ToolError

class TestEnvConsistency(unittest.TestCase):
    def setUp(self):
        # Create a dummy context
        self.product = "mock_product"
        self.release = "mock_release"
        self.variant = "mock_variant"
        self.ctx = BuildContext(self.product, self.release, self.variant)

    def test_restricted_env_vars(self):
        # Should raise ToolError during init
        with self.assertRaises(ToolError) as cm:
            BuildContext("p", "r", "v", env_overrides={"TARGET_PRODUCT": "bad"})
        self.assertIn("reserved and cannot be passed in env_vars", str(cm.exception))

        with self.assertRaises(ToolError) as cm:
            BuildContext("p", "r", "v", env_overrides={"TARGET_RELEASE": "bad"})
        self.assertIn("reserved", str(cm.exception))

        with self.assertRaises(ToolError) as cm:
            BuildContext("p", "r", "v", env_overrides={"TARGET_BUILD_VARIANT": "bad"})
        self.assertIn("reserved", str(cm.exception))

if __name__ == "__main__":
    unittest.main()
