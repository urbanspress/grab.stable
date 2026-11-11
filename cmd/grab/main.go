package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/server"
	"grab/internal/template"
)

const usageText = "usage: grab run [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] <file.grab>\n" +
	"       grab serve [--addr 127.0.0.1:8080] [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] <file.grab> [template.grab.html]\n" +
	"       grab check [--diagnostics beginner|developer|production] <file.grab> [template.grab.html]\n" +
	"       grab format [--write] <file.grab>"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr, nil))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), arguments, stdout, stderr, nil)
}

func runContext(ctx context.Context, arguments []string, stdout, stderr io.Writer, ready chan<- string) int {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}

	switch arguments[0] {
	case "run":
		cfg, positional, ok := parseCommonFlags(arguments[1:], false)
		if !ok || len(positional) != 1 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		filePath := positional[0]
		sourceBytes, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		source := string(sourceBytes)
		program, err := parser.Parse(filePath, source)
		if err != nil {
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(source, err, cfg.diagMode))
			return 1
		}
		if err := interpreter.RunWithOptions(program, stdout, cfg.opts); err != nil {
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(source, err, cfg.diagMode))
			return 1
		}
		return 0

	case "serve":
		cfg, positional, ok := parseCommonFlags(arguments[1:], true)
		if !ok || len(positional) < 1 || len(positional) > 2 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		grabFile := positional[0]
		var tmplFile string
		if len(positional) == 2 {
			tmplFile = positional[1]
		}
		if err := server.ServeWithOptions(ctx, cfg.addr, grabFile, tmplFile, cfg.opts, stdout, ready); err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		return 0

	case "check":
		cfg, positional, ok := parseCommonFlags(arguments[1:], false)
		if !ok || len(positional) < 1 || len(positional) > 2 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		filePath := positional[0]
		sourceBytes, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		source := string(sourceBytes)
		program, err := parser.Parse(filePath, source)
		if err != nil {
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(source, err, cfg.diagMode))
			return 1
		}
		bound, err := interpreter.Validate(program)
		if err != nil {
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(source, err, cfg.diagMode))
			return 1
		}
		if len(positional) == 2 {
			tmplPath := positional[1]
			tmplBytes, err := os.ReadFile(tmplPath)
			if err != nil {
				fmt.Fprintf(stderr, "grab: %v\n", err)
				return 1
			}
			tmpl, err := template.Parse(tmplPath, string(tmplBytes))
			if err != nil {
				fmt.Fprintf(stderr, "grab: %v\n", err)
				return 1
			}
			if err := template.ValidateBindings(tmpl, bound); err != nil {
				fmt.Fprintf(stderr, "grab: %v\n", err)
				return 1
			}
		}
		return 0

	case "format":
		writeInPlace, positional, ok := parseFormatArgs(arguments[1:])
		if !ok || len(positional) != 1 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		filePath := positional[0]
		sourceBytes, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		source := string(sourceBytes)
		formatted, err := parser.Format(filePath, source)
		if err != nil {
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(source, err, interpreter.ModeDeveloper))
			return 1
		}
		if writeInPlace {
			if err := os.WriteFile(filePath, []byte(formatted), 0o644); err != nil {
				fmt.Fprintf(stderr, "grab: %v\n", err)
				return 1
			}
			return 0
		}
		_, _ = io.WriteString(stdout, formatted)
		return 0

	default:
		fmt.Fprintln(stderr, usageText)
		return 2
	}
}

type commonFlagConfig struct {
	addr     string
	diagMode interpreter.DiagnosticMode
	opts     interpreter.Options
}

func parseCommonFlags(args []string, allowAddr bool) (commonFlagConfig, []string, bool) {
	cfg := commonFlagConfig{
		addr:     server.DefaultAddr,
		diagMode: interpreter.ModeDeveloper,
	}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if allowAddr && (arg == "--addr" || strings.HasPrefix(arg, "--addr=")) {
			val, nextIdx, ok := consumeFlagValue(args, i, "--addr")
			if !ok {
				return cfg, nil, false
			}
			cfg.addr = val
			i = nextIdx
			continue
		}
		if arg == "--diagnostics" || strings.HasPrefix(arg, "--diagnostics=") {
			val, nextIdx, ok := consumeFlagValue(args, i, "--diagnostics")
			if !ok {
				return cfg, nil, false
			}
			mode, valid := interpreter.ParseDiagnosticMode(val)
			if !valid {
				return cfg, nil, false
			}
			cfg.diagMode = mode
			i = nextIdx
			continue
		}
		if arg == "--allow-read" || strings.HasPrefix(arg, "--allow-read=") {
			val, nextIdx, ok := consumeFlagValue(args, i, "--allow-read")
			if !ok {
				return cfg, nil, false
			}
			cfg.opts.Permissions.AllowRead = append(cfg.opts.Permissions.AllowRead, val)
			i = nextIdx
			continue
		}
		if arg == "--allow-write" || strings.HasPrefix(arg, "--allow-write=") {
			val, nextIdx, ok := consumeFlagValue(args, i, "--allow-write")
			if !ok {
				return cfg, nil, false
			}
			cfg.opts.Permissions.AllowWrite = append(cfg.opts.Permissions.AllowWrite, val)
			i = nextIdx
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return cfg, nil, false
		}
		positional = append(positional, arg)
	}
	return cfg, positional, true
}

func parseFormatArgs(args []string) (bool, []string, bool) {
	writeInPlace := false
	var positional []string
	for _, arg := range args {
		if arg == "--write" {
			writeInPlace = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return false, nil, false
		}
		positional = append(positional, arg)
	}
	return writeInPlace, positional, true
}

func consumeFlagValue(args []string, idx int, flagName string) (string, int, bool) {
	arg := args[idx]
	prefix := flagName + "="
	if strings.HasPrefix(arg, prefix) {
		val := strings.TrimPrefix(arg, prefix)
		if val == "" {
			return "", idx, false
		}
		return val, idx, true
	}
	if arg == flagName {
		if idx+1 >= len(args) || args[idx+1] == "" {
			return "", idx, false
		}
		return args[idx+1], idx + 1, true
	}
	return "", idx, false
}
