package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"grab/internal/mcp"
)

func runMCP(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var cfg mcp.Config
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		name, eqVal, hasEq := strings.Cut(arg, "=")
		nextVal := func() (string, bool) {
			if hasEq {
				return eqVal, eqVal != ""
			}
			if i+1 >= len(arguments) {
				return "", false
			}
			i++
			return arguments[i], arguments[i] != ""
		}
		switch name {
		case "--workspace":
			v, ok := nextVal()
			if !ok {
				fmt.Fprintln(stderr, usageText)
				return 2
			}
			cfg.Workspace = v
		case "--allow-write":
			if hasEq {
				fmt.Fprintln(stderr, usageText)
				return 2
			}
			cfg.AllowWrite = true
		case "--db-config":
			v, ok := nextVal()
			if !ok {
				fmt.Fprintln(stderr, usageText)
				return 2
			}
			cfg.DBConfig = v
		case "--platform-config":
			v, ok := nextVal()
			if !ok {
				fmt.Fprintln(stderr, usageText)
				return 2
			}
			cfg.PlatformConfig = v
		case "--api-contract":
			v, ok := nextVal()
			if !ok {
				fmt.Fprintln(stderr, usageText)
				return 2
			}
			cfg.APIContract = v
		default:
			fmt.Fprintln(stderr, usageText)
			return 2
		}
	}

	srv, err := mcp.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "grab mcp: %v\n", err)
		return 1
	}
	if err := srv.ServeStdio(ctx, stdin, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "grab mcp: %v\n", err)
		return 1
	}
	return 0
}
