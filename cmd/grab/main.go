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
)

const usageText = "usage: grab run <file.grab>\n       grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]"

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
		if len(arguments) != 2 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		source, err := os.ReadFile(arguments[1])
		if err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		program, err := parser.Parse(arguments[1], string(source))
		if err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		if err := interpreter.Run(program, stdout); err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		return 0

	case "serve":
		addr, positional, ok := parseServeArgs(arguments[1:])
		if !ok || len(positional) < 1 || len(positional) > 2 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		grabFile := positional[0]
		var tmplFile string
		if len(positional) == 2 {
			tmplFile = positional[1]
		}
		if err := server.Serve(ctx, addr, grabFile, tmplFile, stdout, ready); err != nil {
			fmt.Fprintf(stderr, "grab: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintln(stderr, usageText)
		return 2
	}
}

func parseServeArgs(args []string) (string, []string, bool) {
	addr := server.DefaultAddr
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--addr" {
			if i+1 >= len(args) || args[i+1] == "" {
				return "", nil, false
			}
			addr = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--addr=") {
			val := strings.TrimPrefix(arg, "--addr=")
			if val == "" {
				return "", nil, false
			}
			addr = val
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", nil, false
		}
		positional = append(positional, arg)
	}
	return addr, positional, true
}
