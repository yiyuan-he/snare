"""
Python AST helper for snare.

Parses Python source files and extracts function metadata as JSON.
Invoked by snare's Go code via `python3 -c` with this script embedded.

Usage:
    python3 python_helper.py <file_path>

Output: JSON array of function objects:
[
  {
    "name": "function_name",
    "signature": "def function_name(arg1, arg2):",
    "body": "full function source including def line",
    "start_line": 10,
    "end_line": 25,
    "imports": ["import os", "from typing import List"],
    "module": "module_name"
  }
]
"""

import ast
import json
import sys
import textwrap


def extract_functions(file_path):
    """Extract function metadata from a Python source file."""
    with open(file_path, "r") as f:
        source = f.read()

    try:
        tree = ast.parse(source, filename=file_path)
    except SyntaxError as e:
        print(json.dumps({"error": str(e)}), file=sys.stderr)
        sys.exit(1)

    lines = source.splitlines(keepends=True)

    # Extract imports
    imports = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                if alias.asname:
                    imports.append(f"import {alias.name} as {alias.asname}")
                else:
                    imports.append(f"import {alias.name}")
        elif isinstance(node, ast.ImportFrom):
            names = ", ".join(
                f"{a.name} as {a.asname}" if a.asname else a.name
                for a in node.names
            )
            module = node.module or ""
            imports.append(f"from {module} import {names}")

    # Determine module name from file path
    module = file_path.replace("/", ".").replace("\\", ".")
    if module.endswith(".py"):
        module = module[:-3]
    # Use just the filename without extension as module name
    parts = file_path.replace("\\", "/").split("/")
    module = parts[-1].replace(".py", "") if parts else module

    functions = []

    for node in ast.iter_child_nodes(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            func_info = _extract_func(node, lines, imports, module)
            if func_info:
                functions.append(func_info)
        elif isinstance(node, ast.ClassDef):
            # Extract methods from classes
            for item in ast.iter_child_nodes(node):
                if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    func_info = _extract_func(
                        item, lines, imports, module,
                        class_name=node.name
                    )
                    if func_info:
                        functions.append(func_info)

    return functions


def _extract_func(node, lines, imports, module, class_name=None):
    """Extract metadata for a single function/method node."""
    start_line = node.lineno
    end_line = node.end_lineno or node.lineno

    # Get the function source from the original lines
    func_lines = lines[start_line - 1:end_line]
    body = "".join(func_lines)

    # Build signature from the def line
    # Find the first line that contains 'def '
    sig_lines = []
    for i, line in enumerate(func_lines):
        sig_lines.append(line)
        if ":" in line and not line.strip().startswith("#"):
            # Check if the colon is at the end (possibly after stripping comments)
            stripped = line.split("#")[0].rstrip()
            if stripped.endswith(":"):
                break

    signature = "".join(sig_lines).rstrip()

    # Build qualified name
    name = node.name
    if class_name:
        name = f"{class_name}.{node.name}"

    return {
        "name": name,
        "signature": signature,
        "body": body,
        "start_line": start_line,
        "end_line": end_line,
        "imports": imports,
        "module": module,
    }


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: python3 python_helper.py <file_path>", file=sys.stderr)
        sys.exit(1)

    file_path = sys.argv[1]
    functions = extract_functions(file_path)
    print(json.dumps(functions, indent=2))
