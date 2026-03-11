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

import argparse
import os
import sys
import json
from typing import Any, Optional
import traceback
from pathlib import Path
import io
import contextlib

# Add the SDK root to sys.path for absolute imports
# This file is in interface/cli.py, so root is two levels up
sdk_root = Path(__file__).resolve().parent.parent
if str(sdk_root) not in sys.path:
    sys.path.insert(0, str(sdk_root))

from interface.errors import ToolError

from api.env import BuildContext
# Import defs to ensure tools are registered
import interface.defs
from interface.registry import TOOLS
from interface.server import MCPServer

def generate_mcp_config_data() -> dict[str, Any]:
    """Generates the MCP configuration dictionary."""
    # Note: We do not need to list tools here. The Agent discovers tools dynamically at runtime.
    # The output structure is for mcpServers configuration.

    return {
      "mcpServers": {
        "android_build": {
          "command": "python3",
          "args": [
            "${ANDROID_BUILD_TOP}/build/soong/scripts/ai/android_build/interface/cli.py"
          ],
          "env": {
            "PYTHONPATH": "${ANDROID_BUILD_TOP}/build/soong/scripts/ai/android_build"
          }
        }
      }
    }

def main() -> None:
    # If no arguments are provided, run as MCP Server
    if len(sys.argv) == 1:
        server = MCPServer()
        server.run()
        sys.exit(0)

    parser = argparse.ArgumentParser(description="Android Build AI Agent Interface")
    parser.add_argument("--debug", action="store_true", help="Enable debug output (tracebacks)")
    parser.add_argument("--print-registration-cmd", action="store_true", help="Print the command to register this MCP server")
    parser.add_argument("--generate-mcp-config", action="store_true", help="Generate the mcp.json configuration file")
    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    # Dynamically generate subcommands from the registry
    for tool_name in TOOLS:
        subparser = subparsers.add_parser(tool_name, help=f"Run {tool_name}")
        # Note: --args-json is removed from mcp.json generation but required for CLI execution by Agent
        subparser.add_argument("--args-json", type=str, required=True, help="JSON arguments for the tool")

    args = parser.parse_args()

    # Path to the current script
    current_script = Path(__file__).resolve()
    # SDK root is two levels up from interface/cli.py
    if current_script.parent.name == "interface":
        sdk_root = current_script.parent.parent
    else:
        sdk_root = current_script.parent

    # settings.json location: Current Working Directory for output
    settings_file = Path.cwd() / "settings.json"

    if args.generate_mcp_config:
        config_data = generate_mcp_config_data()
        with open(settings_file, "w") as f:
            json.dump(config_data, f, indent=2)
        print(f"Generated {settings_file}")
        sys.exit(0)

    if args.print_registration_cmd:
        # Generate config in-memory
        config_data = generate_mcp_config_data()

        server_config = config_data.get("mcpServers", {}).get("android_build")
        if not server_config:
            print("Error: Invalid configuration (missing 'mcpServers.android_build').", file=sys.stderr)
            sys.exit(1)

        android_build_top = os.getenv('ANDROID_BUILD_TOP', sdk_root.parent.parent.parent.parent.parent)

        cmd = server_config["command"]
        script_args = [arg.replace("${ANDROID_BUILD_TOP}", str(android_build_top)) for arg in server_config["args"]]
        env_vars = {k: v.replace("${ANDROID_BUILD_TOP}", str(android_build_top)) for k, v in server_config.get("env", {}).items()}
        # Format the command according to gemini mcp add help:
        # gemini mcp add [options] <name> <commandOrUrl> [args...]

        # Positional arguments: name, commandOrUrl, args...
        name = "android_build"
        command_or_url = cmd
        args_str = " ".join([f"'{arg}'" for arg in script_args])

        # Options
        env_str = " ".join([f"-e {k}='{v}'" for k, v in env_vars.items()])

        print(f"gemini mcp add {name} {command_or_url} {args_str} {env_str} --trust")
        sys.exit(0)

    if not args.command:
        parser.print_help()
        sys.exit(1)

    tool_def = TOOLS[args.command]

    debug_mode = args.debug

    try:
        data = json.loads(args.args_json)
        tool_args_obj = tool_def.args_model.from_dict(data)

        if hasattr(tool_args_obj, 'product') and hasattr(tool_args_obj, 'release') and hasattr(tool_args_obj, 'variant'):
             args_env_vars = getattr(tool_args_obj, 'env_vars', None)
             ctx = BuildContext(tool_args_obj.product, tool_args_obj.release, tool_args_obj.variant, args_env_vars)
        else:
             raise ToolError(f"Tool arguments for '{args.command}' must contain product, release, and variant.")

        output_text = ""
        error_text = ""
        f_out = io.StringIO()
        f_err = io.StringIO()

        def progress_callback(current: float, total: Optional[float] = None) -> None:
            msg = {"mcp_type": "progress", "current": current}
            if total is not None:
                msg["total"] = total
            sys.__stderr__.write(json.dumps(msg) + "\n")
            sys.__stderr__.flush()

        try:
            with contextlib.redirect_stdout(f_out), contextlib.redirect_stderr(f_err):
                try:
                    result = tool_def.implementation(ctx, tool_args_obj, progress_callback=progress_callback)
                except TypeError as e:
                    if "unexpected keyword argument 'progress_callback'" in str(e):
                        result = tool_def.implementation(ctx, tool_args_obj)
                    else:
                        raise e

        finally:
            output_text = f_out.getvalue()
            error_text = f_err.getvalue()

        # Build output envelope
        response = {
            "status": "success",
            "result": result
        }
        if output_text:
            response["stdout"] = output_text
        if error_text:
            response["stderr"] = error_text

        print(json.dumps(response, indent=2))

    except Exception as e:
        error_response = {
            "status": "error",
            "message": str(e)
        }
        if debug_mode:
            error_response["traceback"] = traceback.format_exc()
        print(json.dumps(error_response, indent=2))
        sys.exit(1)

if __name__ == "__main__":
    main()
