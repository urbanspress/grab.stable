package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func runInit(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runInitWithLocator(ctx, args, stdin, stdout, stderr, locateGrabmanBinary)
}

func runInitWithLocator(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, locate func() string) int {
	grabmanBin := ""
	if locate != nil {
		grabmanBin = locate()
	}
	if grabmanBin == "" {
		fmt.Fprintln(stderr, "grab: 'grabman' CLI was not found on PATH or standard install locations.")
		fmt.Fprintln(stderr, "Install grabman >= v1.1.0 from https://github.com/urbanspress/grabman.stable/releases (Windows: C:\\Program Files\\GRAB\\grabman.exe; Linux: /usr/local/bin/grabman) or set GRABMAN_BIN.")
		return 1
	}

	cmdArgs := append([]string{"init"}, args...)
	cmd := exec.CommandContext(ctx, grabmanBin, cmdArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "grab: failed running %s init: %v\n", grabmanBin, err)
		return 1
	}
	return 0
}

func locateGrabmanBinary() string {
	candidates := []string{}
	if envBin := strings.TrimSpace(os.Getenv("GRABMAN_BIN")); envBin != "" {
		candidates = append(candidates, envBin)
	}
	if exePath, err := os.Executable(); err == nil && exePath != "" {
		name := "grabman"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), name))
	}
	if p, err := exec.LookPath("grabman"); err == nil && p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		`C:\Program Files\GRAB\grabman.exe`,
		"/usr/local/bin/grabman",
		"/usr/bin/grabman",
	)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
