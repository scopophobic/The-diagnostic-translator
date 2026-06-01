package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultTimeoutMs = 120000
	maxOutputBytes   = 16000
)

const version = "0.1.1"

func main() {
	if len(os.Args) > 1 {
		cliMode()
		return
	}
	mcpMode()
}

func cliMode() {
	rawArgs := os.Args[1:]
	timeoutMs := defaultTimeoutMs
	args := rawArgs

loop:
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "--timeout", "-t":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "error: --timeout requires a value in milliseconds")
				os.Exit(1)
			}
			val, err := strconv.Atoi(args[1])
			if err != nil || val <= 0 {
				fmt.Fprintln(os.Stderr, "error: --timeout must be a positive integer (milliseconds)")
				os.Exit(1)
			}
			timeoutMs = val
			args = args[2:]
			continue loop
		case "--":
			args = args[1:]
			break loop
		case "-v", "--version":
			fmt.Printf("ZeroTest v%s\n", version)
			return
		case "-h", "--help":
			printCLIHelp()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) == 0 {
		printCLIHelp()
		return
	}

	cmd := args[0]
	cmdArgs := args[1:]

	report := runAndDiagnose(context.Background(), cmd, cmdArgs, "", timeoutMs, "", "")

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding output: %v\n", err)
		os.Exit(1)
	}
	if !report.Ok {
		os.Exit(report.RuntimeMetadata.ExitCode)
	}
}

func printCLIHelp() {
	fmt.Printf(`ZeroTest v%s - diagnostic JSON for any command output

Usage:
  zerotest [flags] <command> [args...]

Flags:
  -t, --timeout <ms>    Kill the command after <ms> and return diagnostics so far
                         (default 120000). Useful for servers like Django runserver
                         that print errors but never exit.
  -h, --help            Show this help
  -v, --version         Show version

Examples:
  zerotest python -m mypy src/app.py
  zerotest go build ./...
  zerotest tsc --noEmit
  zerotest gcc main.c -o main
  zerotest --timeout 5000 python manage.py runserver
`, version)
}

func mcpMode() {
	s := server.NewMCPServer(
		"ZeroTest",
		version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	tool := mcp.NewTool("diagnose_command",
		mcp.WithDescription("Run a command and return structured diagnostics from its output"),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Executable to run, for example: python"),
		),
		mcp.WithArray("args",
			mcp.Description("Arguments to pass to the command"),
			mcp.WithStringItems(),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for the command"),
		),
		mcp.WithInteger("timeout_ms",
			mcp.Description("Timeout in milliseconds"),
		),
		mcp.WithString("language_hint",
			mcp.Description("Optional language override (e.g. python, go, rust, java, cpp, typescript, ruby)"),
		),
		mcp.WithString("toolchain_hint",
			mcp.Description("Optional toolchain override (e.g. mypy, go, tsc, rustc, gcc, javac, eslint)"),
		),
	)

	s.AddTool(tool, diagnoseHandler)

	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

func diagnoseHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := request.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	args := request.GetStringSlice("args", nil)
	if len(args) == 0 && strings.Contains(command, " ") {
		fields := strings.Fields(command)
		command = fields[0]
		args = fields[1:]
	}

	cwd := request.GetString("cwd", "")
	timeoutMs := request.GetInt("timeout_ms", defaultTimeoutMs)
	languageHint := strings.TrimSpace(request.GetString("language_hint", ""))
	toolchainHint := strings.TrimSpace(request.GetString("toolchain_hint", ""))

	report := runAndDiagnose(ctx, command, args, cwd, timeoutMs, languageHint, toolchainHint)
	return mcp.NewToolResultStructuredOnly(report), nil
}

func runAndDiagnose(ctx context.Context, command string, args []string, cwd string, timeoutMs int, languageHint string, toolchainHint string) DiagnosticReport {
	start := time.Now()

	execCtx := ctx
	var cancel context.CancelFunc
	if timeoutMs > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	execMs := time.Since(start).Milliseconds()

	exitCode := 0
	if runErr != nil {
		if exitErr := new(exec.ExitError); errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(runErr, context.DeadlineExceeded) {
			exitCode = -1
		} else {
			exitCode = 1
		}
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	output := stderrStr
	if strings.TrimSpace(output) == "" {
		output = stdoutStr
	}

	language := languageHint
	if language == "" {
		language = detectLanguage(command, output)
	}

	toolchain := toolchainHint
	if toolchain == "" {
		toolchain = detectToolchain(command, output)
	}

	diagnostics := parseDiagnostics(output, language)

	if errors.Is(runErr, context.DeadlineExceeded) {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "DT_TIMEOUT",
			Severity: "error",
			Message:  "Command timed out before completion",
		})
	}

	if runErr != nil && len(diagnostics) == 0 {
		message := strings.TrimSpace(output)
		if message == "" {
			message = runErr.Error()
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "DT_GENERIC",
			Severity: "error",
			Message:  message,
		})
	}

	return DiagnosticReport{
		Ok: runErr == nil,
		RuntimeMetadata: RuntimeMetadata{
			Language:        language,
			Toolchain:       toolchain,
			ExecutionTimeMs: execMs,
			ExitCode:        exitCode,
			Command:         command,
			Args:            args,
			Cwd:             cwd,
		},
		Diagnostics: diagnostics,
		RawOutput:   truncateOutput(output, maxOutputBytes),
		Stdout:      truncateOutput(stdoutStr, maxOutputBytes),
		Stderr:      truncateOutput(stderrStr, maxOutputBytes),
	}
}
