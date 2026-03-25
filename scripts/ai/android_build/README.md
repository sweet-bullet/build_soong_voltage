# Android Build AI SDK

A pure Python SDK providing a stateless, Model Context Protocol (MCP) compliant interface for AI Agents to interact with the Android Build System and the CLI tools (Soong, Ninja, aconfig, etc.).

## Features

- **Stateless Environment Management**: Automatically captures and caches `envsetup.sh` and `lunch` results. No manual environment sourcing required.
- **MCP & JSON-RPC 2.0 Compliant**: Supports standard tool discovery, batch requests, and progress notifications.
- **Structured Build Outcomes**: Build results (including compilation failures) are returned as data objects rather than exceptions, allowing the Agent to programmatically analyze logs and exit codes.
- **Dynamic Tool Discovery**: The tool list is generated at runtime from a central registry. Adding new build system capabilities is as simple as defining a new function and registering it with a decorator.
- **Isolated Testing**: Unit tests run using mocks and temporary `OUT_DIR` redirection to avoid workspace pollution.
- **No External Dependencies**: Uses only the Python standard library.

## Project Structure

```text
android_build/
├── api/                # Layer 1: Core Build System APIs
│   ├── build.py        # Build execution, progress parsing, and structured log analysis
│   ├── config.py       # Build variables and aconfig queries
│   ├── env.py          # Environment snapshotting and BuildContext
│   ├── module.py       # module-info.json parsing and cache management
│   └── ninja.py        # Ninja graph queries, dependency analysis, and command retrieval
├── interface/          # Layer 2: MCP/CLI Interface
│   ├── cli.py          # Unified entry point and server daemon
│   ├── server.py       # JSON-RPC 2.0 Server with Batch/Progress support
│   ├── registry.py     # Tool registration and discovery logic
│   └── schema.py       # Auto-generation of JSON schemas from dataclasses
├── tests/              # Isolated Unit Tests
├── settings.json       # Portable MCP configuration template
└── run_tests.sh        # Test execution script
```

## Setup & Registration

### Automatic Registration (Recommended)
This SDK is integrated into the standard Googlers' developer workflow. When launching the shared MCP server, the SDK is automatically available as the MCP tools.

### Manual Registration
If you need to register the server manually (e.g., for an external MCP client or troubleshooting), use the built-in helper:

1. **Generate the registration command**:
   ```bash
   python3 interface/cli.py --print-registration-cmd
   ```

2. **Execute the output command**:
   The output will follow the correct positional syntax:
   ```bash
   gemini mcp add android_build python3 '/path/to/cli.py' -e PYTHONPATH='/path/to/sdk' --trust
   ```

## Manual Usage (Server Mode)

You can interact with the server manually via stdin/stdout:

```bash
echo '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}' | python3 interface/cli.py
```

## Development & Testing

To run the full suite of isolated unit tests:

```bash
source build/envsetup.sh && lunch <lunch product> && atest -c build_sdk_lib_test
```
