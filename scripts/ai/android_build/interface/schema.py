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
import inspect
import typing
import re
from typing import Any, Optional, Callable

# Define mapping outside the loop for efficiency
TYPE_MAP: dict[type, str] = {
    str: "string",
    int: "integer",
    float: "number",
    bool: "boolean",
    list: "array",
    dict: "object"
}

def _parse_docstring_args(docstring: Optional[str]) -> dict[str, str]:
    """
    Parses a Google-style docstring to extract argument descriptions.
    Returns a dict mapping argument names to their descriptions.
    """
    if not docstring:
        return {}

    args_descriptions = {}
    lines = docstring.splitlines()
    in_args_section = False
    current_arg = None

    # Regex to match "  arg_name (type): description" or "  arg_name: description"
    # We ignore the type in parenthesis if present, as we use type hints.
    arg_pattern = re.compile(r"^\s+(?P<name>\w+)(?:\s*\(.*\))?:\s*(?P<desc>.*)$")

    for line in lines:
        stripped_line = line.strip()

        if stripped_line == "Args:":
            in_args_section = True
            continue

        if in_args_section:
            # Check if we've exited the Args section (empty line or new section)
            # But empty lines are allowed in Args. New section usually starts without indentation.
            # However, simpler heuristic: if line is unindented and not empty, we are done.
            if line and not line.startswith(" "):
                 break

            match = arg_pattern.match(line)
            if match:
                current_arg = match.group("name")
                args_descriptions[current_arg] = match.group("desc")
            elif current_arg and stripped_line:
                # Continuation of description
                args_descriptions[current_arg] += " " + stripped_line

    return args_descriptions

def get_docstring_summary(docstring: Optional[str]) -> str:
    """
    Returns the summary part of a docstring (text before 'Args:').
    """
    if not docstring:
        return "No description provided."

    # Split by 'Args:' and take the first part
    summary = docstring.split("Args:")[0].strip()
    return summary

@dataclasses.dataclass(frozen=True)
class ToolArgs:
    """Base class for tool arguments."""

    @classmethod
    def get_json_schema(cls, func: Optional[Callable[..., Any]] = None) -> dict[str, Any]:
        """
        Generates an OpenAI-compatible JSON schema for the arguments.
        Prioritizes descriptions from the function docstring (if provided),
        falling back to dataclass field metadata.
        """
        properties: dict[str, Any] = {}
        required: list[str] = []

        doc_args = {}
        if func:
            doc_args = _parse_docstring_args(inspect.getdoc(func))

        for field in dataclasses.fields(cls):
            field_type = typing.get_origin(field.type) or field.type
            field_name = field.name

            # Concise handling for Optional/Union types
            if field_type is typing.Union:
                # Find the first non-None type in the Union
                args = typing.get_args(field.type)
                field_type = next((t for t in args if t is not type(None)), field_type)

            # Check if it's a generic alias (e.g. list[str], dict[str, str]) and get base type
            origin = typing.get_origin(field_type)
            if origin:
                field_type = origin

            # Priority 1: Function Docstring
            description = doc_args.get(field_name)

            # Priority 2: Field Metadata (Fallback)
            if not description:
                description = field.metadata.get("description", "No description provided.")

            # Map Python types to JSON schema types
            # We use the type directly if it's in the map, otherwise string
            # Handle the case where field_type might not be a class (e.g. Any)
            json_type = "string"
            if isinstance(field_type, type):
                json_type = TYPE_MAP.get(field_type, "string")

            properties[field.name] = {
                "type": json_type,
                "description": description
            }

            # Check if field is required (no default value)
            if field.default == dataclasses.MISSING and field.default_factory == dataclasses.MISSING:
                required.append(field_name)

        return {
            "type": "object",
            "properties": properties,
            "required": required
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ToolArgs":
        """Instantiates the class from a dictionary."""
        # Get field names to filter out extra keys
        field_names = {f.name for f in dataclasses.fields(cls)}
        filtered_data = {k: v for k, v in data.items() if k in field_names}
        return cls(**filtered_data)
