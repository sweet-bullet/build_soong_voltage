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

import sys
import json
import logging
import traceback
import io
import contextlib
import inspect
import dataclasses
from typing import Any, Optional, Union
from api.env import BuildContext
from interface.registry import TOOLS
from interface.schema import get_docstring_summary
from interface.errors import ToolError
import interface.defs

# Configure logging to stderr to avoid corrupting stdout
logging.basicConfig(stream=sys.stderr, level=logging.INFO, format='[MCPServer] %(message)s')

@dataclasses.dataclass(frozen=True)
class JsonRpcResponse:
    id: Any
    result: Any
    jsonrpc: str = "2.0"

@dataclasses.dataclass(frozen=True)
class JsonRpcErrorResponse(JsonRpcResponse):
    """
    Represents a tool or protocol error (masked as success with isError: True).
    The result object MUST contain isError: True.
    """
    def __init__(self, id: Any, code: int, message: str, content: Optional[list[dict[str, Any]]] = None):
        if content is None:
            content = [{"type": "text", "text": f"Error {code}: {message}"}]
        result = {
            "content": content,
            "isError": True
        }
        object.__setattr__(self, "id", id)
        object.__setattr__(self, "result", result)
        object.__setattr__(self, "jsonrpc", "2.0")

@dataclasses.dataclass(frozen=True)
class JsonRpcNotification:
    method: str
    params: dict[str, Any]
    jsonrpc: str = "2.0"

class MCPServer:
    def __init__(self) -> None:
        self._shutdown_received = False
        # Keep reference to original stdout for protocol messages,
        # bypassing redirection during tool execution
        self.stdout = sys.stdout

    def run(self) -> None:
        """Starts the JSON-RPC 2.0 server loop."""
        logging.info("Starting MCP Server loop...")

        while True:
            try:
                line = sys.stdin.readline()
                if not line:
                    break # EOF

                request_data = json.loads(line)

                # Handle Batch Request (list) or Single Request (dict)
                if isinstance(request_data, list):
                    batch_responses = []
                    for req in request_data:
                        response = self.process_request(req)
                        if response:
                            batch_responses.append(response)

                    if batch_responses:
                        self._write_json(batch_responses)

                elif isinstance(request_data, dict):
                    response = self.process_request(request_data)
                    if response:
                        self._write_json(response)
                else:
                    logging.error("Invalid JSON-RPC request: must be object or array")

            except json.JSONDecodeError:
                logging.error("Invalid JSON received.")
                # We can't reply if we can't parse
                continue
            except Exception:
                logging.error(f"Unexpected error in main loop:\n{traceback.format_exc()}")

    def process_request(self, request: dict[str, Any]) -> Optional[JsonRpcResponse]:
        """
        Processes a single JSON-RPC request and returns the response object (or None for notifications).
        """
        request_id = request.get("id")
        method = request.get("method")
        params = request.get("params", {})

        try:
            if method == "initialize":
                return self.create_response(request_id, {
                    "capabilities": {"tools": {}},
                    "protocolVersion": "2024-11-05",
                    "serverInfo": {"name": "android_build", "version": "0.1.0"}
                })

            elif method == "notifications/initialized":
                return None

            elif method == "ping":
                return self.create_response(request_id, {})

            elif method == "shutdown":
                self._shutdown_received = True
                return self.create_response(request_id, None)

            elif method == "exit":
                if self._shutdown_received:
                    sys.exit(0)
                else:
                    sys.exit(1)

            elif method == "tools/list":
                tools_list = []
                for tool_name, tool_def in TOOLS.items():
                    # Use the wrapped API function for docs if available, otherwise the wrapper
                    doc_func = tool_def.wrapped_func or tool_def.implementation

                    schema = tool_def.args_model.get_json_schema(doc_func)
                    doc = inspect.getdoc(doc_func)
                    # Use only the summary (pre-Args) for the high-level tool description
                    summary = get_docstring_summary(doc)
                    # Replace newlines with spaces and collapse multiple spaces
                    description = " ".join(summary.split())

                    tools_list.append({
                        "name": tool_name,
                        "description": description,
                        "inputSchema": schema
                    })
                return self.create_response(request_id, {"tools": tools_list})

            elif method == "tools/call":
                return self.handle_tool_call(request_id, params)

            else:
                if request_id is not None:
                     return self.create_error(request_id, -32601, f"Method not found: {method}")
                return None

        except Exception as e:
            logging.error(f"Error handling request {method}: {e}\n{traceback.format_exc()}")
            if request_id is not None:
                return self.create_error(request_id, -32000, str(e))
            return None

    def handle_tool_call(self, request_id: Any, params: dict[str, Any]) -> JsonRpcResponse:
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        # Spec: "To request progress notifications, the client MUST include a progressToken property in the params of the request."
        progress_token = params.get("progressToken")

        if tool_name not in TOOLS:
            # We treat "Tool not found" as a generic error too, to be safe.
            return self.create_error(request_id, -32601, f"Tool not found: {tool_name}")

        tool_def = TOOLS[tool_name]

        try:
            # Hydrate arguments
            tool_args_obj = tool_def.args_model.from_dict(arguments)

            # Hydrate Context
            if hasattr(tool_args_obj, 'product') and hasattr(tool_args_obj, 'release') and hasattr(tool_args_obj, 'variant'):
                 env_overrides = getattr(tool_args_obj, 'env_vars', None)
                 ctx = BuildContext(tool_args_obj.product, tool_args_obj.release, tool_args_obj.variant, env_overrides=env_overrides)
            else:
                 raise ToolError("Tool arguments must contain product, release, and variant.")

            # Prepare Progress Callback
            progress_callback = None
            if progress_token is not None:
                def progress_callback(current: float, total: Optional[float] = None) -> None:
                    self.send_progress(progress_token, current, total)

            # Capture Output
            output_text = ""
            error_text = ""
            f_out = io.StringIO()
            f_err = io.StringIO()

            try:
                with contextlib.redirect_stdout(f_out), contextlib.redirect_stderr(f_err):
                    tool_def.implementation(ctx, tool_args_obj, progress_callback=progress_callback)
            finally:
                output_text = f_out.getvalue()
                error_text = f_err.getvalue()

            content = []
            if output_text:
                content.append({"type": "text", "text": output_text})
            if error_text:
                content.append({"type": "text", "text": f"--- Stderr ---\n{error_text}"})

            return self.create_response(request_id, {
                "content": content
            })

        except TypeError as e:
            if "unexpected keyword argument 'progress_callback'" in str(e):
                try:
                    output_text = ""
                    error_text = ""
                    f_out = io.StringIO()
                    f_err = io.StringIO()
                    try:
                        with contextlib.redirect_stdout(f_out), contextlib.redirect_stderr(f_err):
                            tool_def.implementation(ctx, tool_args_obj)
                    finally:
                        output_text = f_out.getvalue()
                        error_text = f_err.getvalue()

                    content = []
                    if output_text:
                        content.append({"type": "text", "text": output_text})
                    if error_text:
                        content.append({"type": "text", "text": f"--- Stderr ---\n{error_text}"})
                    return self.create_response(request_id, {"content": content})
                except Exception as inner_e:
                     tb = traceback.format_exc()
                     return self.create_error(request_id, -32603, f"Tool execution failed (fallback): {inner_e}\n{tb}")

            tb = traceback.format_exc()
            return self.create_error(request_id, -32603, f"Tool execution failed: {e}\n{tb}")

        except ToolError as e:
            # Clean Tool Error (no traceback)
            content = [{"type": "text", "text": f"Error -32603: {e}"}]
            if output_text:
                content.append({"type": "text", "text": f"--- Output ---\n{output_text}"})
            if error_text:
                content.append({"type": "text", "text": f"--- Stderr ---\n{error_text}"})
            return self.create_error(request_id, -32603, str(e), content=content)

        except Exception as e:
            # Tool Execution Error (captured as result with isError: True)
            tb = traceback.format_exc()
            content = [{"type": "text", "text": f"Error -32603: Tool execution failed: {e}\n{tb}"}]
            if output_text:
                content.append({"type": "text", "text": f"--- Output ---\n{output_text}"})
            if error_text:
                content.append({"type": "text", "text": f"--- Stderr ---\n{error_text}"})
            return self.create_error(request_id, -32603, str(e), content=content)

    def send_progress(self, progress_token: Any, progress: float, total: Optional[float] = None) -> None:
        params = {
            "progressToken": progress_token,
            "progress": progress
        }
        if total is not None:
            params["total"] = total

        notification = JsonRpcNotification(
            method="notifications/progress",
            params=params
        )
        self._write_json(notification)

    def create_response(self, request_id: Any, result: Any) -> JsonRpcResponse:
        return JsonRpcResponse(id=request_id, result=result)

    def create_error(self, request_id: Any, code: int, message: str, content: Optional[list[dict[str, Any]]] = None) -> JsonRpcErrorResponse:
        return JsonRpcErrorResponse(request_id, code, message, content=content)

    def _write_json(self, data: Any) -> None:
        def default_encoder(obj: Any) -> dict[str, Any]:
            if dataclasses.is_dataclass(obj) and not isinstance(obj, type):
                return dataclasses.asdict(obj)
            raise TypeError(f"Object of type {type(obj).__name__} is not JSON serializable")

        json.dump(data, self.stdout, default=default_encoder)
        self.stdout.write("\n")
        self.stdout.flush()
