# ZeroTest

A lightweight [MCP](https://modelcontextprotocol.io) server that runs commands and converts compiler/linter/runtime errors into structured JSON. Makes error output machine-readable so AI agents can work with it programmatically.

## Supported Languages

| Language | Toolchains |
|----------|-----------|
| Python | mypy, pytest, flake8, pylint, runtime tracebacks |
| Go | go build, go vet, golangci-lint |
| TypeScript | tsc |
| JavaScript | eslint, prettier |
| Rust | rustc, cargo |
| C / C++ | gcc, g++, clang, clang++ |
| Java | javac, maven, gradle |
| Ruby | ruby, rake, rails |

## Install

**Go** (all platforms):
```bash
go install github.com/scopophobic/ZeroTest@latest
```

**macOS / Linux** — curl one-liner:
```bash
curl -sL https://github.com/scopophobic/ZeroTest/releases/latest/download/zerotest-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/') -o /usr/local/bin/zerotest && chmod +x /usr/local/bin/zerotest
```

**Windows** (PowerShell):
```powershell
curl -sL https://github.com/scopophobic/ZeroTest/releases/latest/download/zerotest-windows-amd64.exe -o zerotest.exe
```

> Pre-built binaries are also available on the [releases page](https://github.com/scopophobic/ZeroTest/releases).

## Quick Start

### CLI mode (standalone)

Run any command and get structured JSON diagnostics:

```bash
zerotest python -m mypy src/app.py
zerotest go build ./...
zerotest tsc --noEmit
zerotest gcc main.c -o main
```

The JSON is printed to stdout. Exit code matches the wrapped command.

> **Long-running commands** (e.g., `python manage.py runserver`):  
> Add `--live` to see output in real-time, then press Ctrl+C to stop and get JSON:
> ```bash
> zerotest --live python manage.py runserver
> ```
> Or use `--timeout <ms>` to auto-kill after N milliseconds:
> ```bash
> zerotest --timeout 5000 python manage.py runserver
> ```

### MCP server mode

For use with agents (Claude, Cline, Continue, etc.):

```bash
zerotest
```

Then configure as an MCP tool in your client.

## MCP Client Setup

The server communicates over stdio. Add it to your MCP client config:

**Claude Desktop** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "zerotest": {
      "command": "/path/to/zerotest",
      "args": []
    }
  }
}
```

**VS Code / Cline / Continue**:
```json
{
  "command": "/path/to/zerotest",
  "args": []
}
```

## Usage

The server exposes one tool called **`diagnose_command`**:

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `command` | string | yes | Executable to run (e.g. `python`, `tsc`, `gcc`) |
| `args` | string[] | no | Arguments to pass |
| `cwd` | string | no | Working directory |
| `timeout_ms` | integer | no | Timeout in ms (default 120000) |
| `language_hint` | string | no | Override language detection |
| `toolchain_hint` | string | no | Override toolchain detection |

### Example

```json
{
  "command": "python",
  "args": ["-m", "mypy", "src/app.py"],
  "timeout_ms": 60000
}
```

### Response

```json
{
  "ok": false,
  "runtime_metadata": {
    "language": "python",
    "toolchain": "mypy",
    "execution_time_ms": 14,
    "exit_code": 1,
    "command": "python",
    "args": ["-m", "mypy", "src/app.py"]
  },
  "diagnostics": [
    {
      "code": "DT_PY_OPERATOR",
      "severity": "error",
      "message": "Unsupported operand types for +: \"int\" and \"str\"",
      "location": {
        "file": "src/app.py",
        "line": 42,
        "column": 18
      }
    }
  ],
  "raw_output": "src/app.py:42:18: error: Unsupported operand types...",
  "stdout": "",
  "stderr": "src/app.py:42:18: error: Unsupported operand types..."
}
```

### Response Fields

- `ok` — whether the command exited successfully
- `runtime_metadata` — language, toolchain, execution time, exit code, command
- `diagnostics[]` — parsed issues from stderr/stdout
  - `code` — error identifier (e.g. `DT_TS_TS2322`, `DT_RS_E0308`, `DT_GO_BUILD`)
  - `severity` — `error`, `warning`, or `note`
  - `message` — human-readable error text
  - `location` — file path, line number, column (when available)
  - `context` — source line of the error (Python tracebacks only)
- `raw_output`, `stdout`, `stderr` — captured output (truncated if > 16KB)

## Development

```bash
# Run tests
go test ./...

# Build
go build -o zerotest .
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for details on how to add parsers for new languages, report bugs, or submit changes.

## License

[MIT](LICENSE)
