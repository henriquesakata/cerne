# MCP server and demo project

## Overview (MCP and demo)

An MCP client without a shell reads the same surface through **`cerne mcp`** (F-0363): a stdio MCP server whose read-only tools `status`, `brief`, `next`, `search`, `show` and `ask` each return exactly the matching command's `--json` (none writes; `ask` returns the sources, no model call). Register it with `"command": "cerne", "args": ["mcp"]` in a project-scoped config (the client starts it in the project root), or `"args": ["mcp", "--dir", "/path/to/project"]` in a global one. With a shell, call the commands directly.

To see cerne with data before adopting it, **`cerne demo [<dir>]`** (F-0369) builds taskflow, an example project with every lifecycle state, from content embedded in the binary (default `./cerne-demo`; no clone, no shell, no network). It refuses a `<dir>` that exists or sits inside a cerne project, and removes everything it created if a step fails. Never run it inside the project you are working on.
