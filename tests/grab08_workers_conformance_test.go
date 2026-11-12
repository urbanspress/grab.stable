package tests

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type workerLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *workerLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(b)
}
func (w *workerLog) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.b.String() }
func workerPorts(t *testing.T, n int) int {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		base := l.Addr().(*net.TCPAddr).Port
		ls := []net.Listener{l}
		valid := base+n < 65535
		for i := 1; i < n && valid; i++ {
			l, e = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+i))
			if e != nil {
				valid = false
				break
			}
			ls = append(ls, l)
		}
		for _, l := range ls {
			l.Close()
		}
		if valid {
			return base
		}
	}
	t.Fatal("no available consecutive ports")
	return 0
}
func TestGRAB08WorkerProcesses(t *testing.T) {
	tool, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	binary := filepath.Join(root, "grab.exe")
	build := exec.Command(tool, "build", "-o", binary, "./cmd/grab")
	build.Dir = ".."
	if raw, e := build.CombinedOutput(); e != nil {
		t.Fatalf("%s %v", raw, e)
	}
	client := &http.Client{Timeout: 300 * time.Millisecond}
	defer client.CloseIdleConnections()
	for _, n := range []int{1, 2, 4} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			dir := t.TempDir()
			program := filepath.Join(dir, "app.grab")
			config := filepath.Join(dir, "grab.platform.json")
			os.WriteFile(program, []byte("use server\nroute \"GET\" \"/\" using request\n give back server.text using 200, \"generation-one\"\nend\n"), 0600)
			os.WriteFile(config, []byte(`{"version":1,"workers":{"max_db_connections":8,"max_redis_connections":8}}`), 0600)
			base := workerPorts(t, n)
			args := []string{"serve", "--platform-config", config, "--workers", strconv.Itoa(n), "--worker-base-port", strconv.Itoa(base), "--addr", fmt.Sprintf("127.0.0.1:%d", base), "--reload", program}
			cmd := exec.Command(binary, args...)
			log := &workerLog{}
			cmd.Stdout = log
			cmd.Stderr = log
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer func() {
				_ = cmd.Process.Kill()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("supervisor did not stop")
				}
			}()
			wait := func(want string) {
				t.Helper()
				deadline := time.Now().Add(20 * time.Second)
				for time.Now().Before(deadline) {
					all := true
					for i := 0; i < n; i++ {
						resp, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", base+i))
						if e != nil {
							all = false
							break
						}
						body, _ := io.ReadAll(resp.Body)
						resp.Body.Close()
						if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != want {
							all = false
							break
						}
					}
					if all {
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
				t.Fatalf("workers did not serve %q: %s", want, log.String())
			}
			wait("generation-one")
			os.WriteFile(program, []byte("use server\nroute \"GET\" \"/\" using request\n give back server.text using 200, \"generation-two\"\nend\n"), 0600)
			wait("generation-two")
			if n > 1 {
				matches := regexp.MustCompile(`worker 0 pid=([0-9]+)`).FindAllStringSubmatch(log.String(), -1)
				if len(matches) == 0 {
					t.Fatal("missing worker process identity", log.String())
				}
				pid, _ := strconv.Atoi(matches[len(matches)-1][1])
				child, e := os.FindProcess(pid)
				if e != nil {
					t.Fatal(e)
				}
				if e = child.Kill(); e != nil {
					t.Fatal(e)
				}
				wait("generation-two")
			}
			// Parent death must not leave serving orphan workers.
			_ = cmd.Process.Kill()
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				alive := false
				for i := 0; i < n; i++ {
					resp, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", base+i))
					if e == nil {
						resp.Body.Close()
						alive = true
					}
				}
				if !alive {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatal("orphan worker still accepts requests")
		})
	}
}
