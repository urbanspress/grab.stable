package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunInit_MissingGrabmanShowsInstallHint(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runInitWithLocator(context.Background(), []string{"--non-interactive"}, nil, &out, &errBuf, func() string {
		return ""
	})
	if code != 1 {
		t.Fatalf("expected exit code 1 when grabman is missing, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "grabman") || !strings.Contains(errBuf.String(), "GRABMAN_BIN") {
		t.Fatalf("expected clear install hint in stderr, got:\n%s", errBuf.String())
	}
}

func TestRunInit_ForwardsArgsAndExitCode(t *testing.T) {
	// Build a minimal helper executable simulating grabman init
	helperDir := t.TempDir()
	helperSrc := filepath.Join(helperDir, "mock_grabman.go")
	src := `package main
import (
	"fmt"
	"os"
	"strings"
)
func main() {
	fmt.Println("MOCK:" + strings.Join(os.Args[1:], "|"))
	for _, a := range os.Args[1:] {
		if a == "--fail-test" {
			os.Exit(3)
		}
	}
}
`
	if err := os.WriteFile(helperSrc, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "grabman"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	helperBin := filepath.Join(helperDir, binName)
	buildOut, err := exec.Command("go", "build", "-o", helperBin, helperSrc).CombinedOutput()
	if err != nil {
		t.Fatalf("failed building mock grabman: %v\n%s", err, string(buildOut))
	}

	var out, errBuf bytes.Buffer
	code := runInitWithLocator(context.Background(), []string{"--dir", "my-site", "--profile", "cms-stack", "--json"}, nil, &out, &errBuf, func() string {
		return helperBin
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%s)", code, errBuf.String())
	}
	want := "MOCK:init|--dir|my-site|--profile|cms-stack|--json"
	if strings.TrimSpace(out.String()) != want {
		t.Fatalf("unexpected forwarded argv output: got %q, want %q", strings.TrimSpace(out.String()), want)
	}

	// Verify exit code forwarding
	out.Reset()
	errBuf.Reset()
	code = runInitWithLocator(context.Background(), []string{"--fail-test"}, nil, &out, &errBuf, func() string {
		return helperBin
	})
	if code != 3 {
		t.Fatalf("expected exit code 3 forwarded from mock grabman, got %d", code)
	}

	// Verify GRABMAN_BIN env override is respected by locateGrabmanBinary
	t.Setenv("GRABMAN_BIN", helperBin)
	if got := locateGrabmanBinary(); got != helperBin {
		t.Fatalf("expected locateGrabmanBinary to honor GRABMAN_BIN=%q, got %q", helperBin, got)
	}
}
