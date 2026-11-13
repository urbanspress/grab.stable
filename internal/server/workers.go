package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"grab/internal/interpreter"
	"grab/internal/platform"
)

// SourceHash includes program/template/config/contract/lock bytes, with bounded
// traversal. It is a consistency check, not an application authentication secret.
func SourceHash(program string, extra []string) (string, error) {
	roots := map[string]bool{}
	paths := map[string]bool{}
	for _, p := range append([]string{program}, extra...) {
		if p == "" {
			continue
		}
		p, e := filepath.Abs(p)
		if e != nil {
			return "", e
		}
		roots[filepath.Dir(p)] = true
	}
	total := int64(0)
	for root := range roots {
		e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".codex" {
					return filepath.SkipDir
				}
				return nil
			}
			if !(strings.HasSuffix(p, ".grab") || strings.HasSuffix(p, ".grab.html") || strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".lock")) {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("worker dependency symlink rejected")
			}
			paths[p] = true
			if len(paths) > 10000 {
				return fmt.Errorf("worker dependency limit")
			}
			return nil
		})
		if e != nil {
			return "", e
		}
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	h := sha256.New()
	for _, p := range ordered {
		st, e := os.Stat(p)
		if e != nil {
			return "", e
		}
		total += st.Size()
		if total > 64<<20 {
			return "", fmt.Errorf("worker dependency limit")
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(raw))
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func executableHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type processWorker struct {
	cmd       *exec.Cmd
	done      chan error
	drain     bool
	heartbeat time.Time
	crashes   []time.Time
	next      time.Time
	exhausted bool
}
type workerControl struct {
	mu      sync.Mutex
	workers []*processWorker
	token   string
}

func (c *workerControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.token)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.URL.Path == "/control" {
		i, e := strconv.Atoi(r.URL.Query().Get("worker"))
		if e != nil || i < 0 || i >= len(c.workers) {
			http.NotFound(w, r)
			return
		}
		p := c.workers[i]
		if p == nil {
			http.Error(w, "unavailable", 503)
			return
		}
		if r.URL.Query().Get("ready") == "false" {
			p.heartbeat = time.Time{}
		} else {
			p.heartbeat = time.Now()
		}
		if p.drain {
			fmt.Fprint(w, "drain")
		} else {
			fmt.Fprint(w, "run")
		}
		return
	}
	if r.URL.Path != "/_ready" && r.URL.Path != "/_health" {
		http.NotFound(w, r)
		return
	}
	for _, p := range c.workers {
		if p == nil || p.exhausted || p.drain || time.Since(p.heartbeat) > 3*time.Second {
			http.Error(w, "capacity unavailable", 503)
			return
		}
	}
	fmt.Fprint(w, "ready\n")
}

type synchronizedOutput struct {
	mu  sync.Mutex
	out io.Writer
}

func (o *synchronizedOutput) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.out.Write(b)
}

func serveWorkers(ctx context.Context, program, template string, opts interpreter.Options, cfg Config, output io.Writer, ready chan<- string) error {
	if cfg.Workers < 2 || cfg.Workers > 32 || cfg.WorkerBasePort < 1 || cfg.WorkerBasePort+cfg.Workers-1 > 65535 {
		return fmt.Errorf("invalid worker count/port range")
	}
	loader := opts.PlatformLoader
	// Pin the validated manifest; Load must not re-read a different pool policy
	// between admission accounting and opening generation resources.
	opts.PlatformLoader = nil
	if loader != nil {
		var e error
		opts.PlatformConfig, opts.PlatformGrants, e = loader()
		if e != nil {
			return e
		}
	}
	if e := opts.PlatformConfig.PoolBudget(cfg.Workers, opts.DataGrants.PoolBudget()); e != nil {
		return e
	}
	policyStart, _ := opts.PlatformConfig.Workers()
	if cfg.Reload && (policyStart.DBHeadroom < opts.DataGrants.PoolBudget() || policyStart.RedisHeadroom < opts.PlatformConfig.RedisPoolBudget()) {
		return fmt.Errorf("rolling validation needs one generation of maintenance pool headroom")
	}
	opts.DeferWorkers = true
	staticPrefix, staticRoot, e := parseStaticConfig(cfg.Static)
	if e != nil {
		return e
	}
	// Validate complete generation and required stores before spawning any process.
	prepared, e := loadWithStatic(program, template, staticPrefix, staticRoot, opts, io.Discard)
	if e != nil {
		return e
	}
	prepared.retire()
	extra := append([]string{opts.PlatformPath, opts.APIContractPath, template}, cfg.WorkerFiles...)
	hash, e := SourceHash(program, extra)
	if e != nil {
		return e
	}
	contractHash, e := contractSnapshot(opts.APIContractPath)
	if e != nil {
		return e
	}
	maintenanceHash, e := workerMaintenanceSnapshot(cfg.WorkerFiles)
	if e != nil {
		return e
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil {
		return e
	}
	execHash, e := executableHash(executable)
	if e != nil {
		return e
	}
	// Reserve every port together; refuse partial startup on a known conflict.
	var reservations []net.Listener
	for i := 0; i < cfg.Workers; i++ {
		l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.WorkerBasePort+i))
		if e != nil {
			for _, l := range reservations {
				l.Close()
			}
			return e
		}
		reservations = append(reservations, l)
	}
	defer func() {
		for _, l := range reservations {
			l.Close()
		}
	}()
	policy, _ := opts.PlatformConfig.Workers()
	listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", policy.ManagementPort))
	if e != nil {
		return e
	}
	defer listener.Close()
	for _, l := range reservations {
		l.Close()
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	token := hex.EncodeToString(secret)
	if policy.TokenEnv != "" {
		var e error
		token, e = platform.Secret(policy.TokenEnv)
		if e != nil || len(token) < 32 {
			return platform.ErrInvalid
		}
	}
	control := &workerControl{workers: make([]*processWorker, cfg.Workers), token: token}
	management := &http.Server{Handler: control, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 8192}
	go management.Serve(listener)
	defer management.Close()
	if output == nil {
		output = io.Discard
	}
	out := &synchronizedOutput{out: output}
	fmt.Fprintf(out, "grab: worker management at http://%s (private bearer authentication)\n", listener.Addr())
	env := restrictedWorkerEnv(opts, cfg)
	spawn := func(i int, p *processWorker) error {
		actual, e := executableHash(executable)
		if e != nil || actual != execHash {
			return fmt.Errorf("worker executable changed")
		}
		cmd := exec.Command(executable, cfg.WorkerArguments...)
		cmd.Env = append(append([]string(nil), env...), "GRAB_WORKER_CONTROL=http://"+listener.Addr().String(), "GRAB_WORKER_TOKEN="+control.token, "GRAB_WORKER_ID="+strconv.Itoa(i), "GRAB_WORKER_RESTARTS="+strconv.Itoa(len(p.crashes)), "GRAB_WORKER_PORT="+strconv.Itoa(cfg.WorkerBasePort+i), "GRAB_WORKER_HASH="+hash)
		cmd.Stdout = out
		cmd.Stderr = out
		p.cmd = cmd
		p.done = make(chan error, 1)
		control.mu.Lock()
		p.heartbeat = time.Time{}
		p.drain = false
		control.mu.Unlock()
		if e = cmd.Start(); e != nil {
			p.cmd = nil
			return e
		}
		fmt.Fprintf(out, "grab: worker %d pid=%d\n", i, cmd.Process.Pid)
		done := p.done
		go func() { done <- cmd.Wait() }()
		return nil
	}
	stop := func(p *processWorker) {
		if p == nil || p.cmd == nil {
			return
		}
		control.mu.Lock()
		p.drain = true
		control.mu.Unlock()
		select {
		case <-p.done:
		case <-time.After(31 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
		}
		p.cmd = nil
	}
	defer func() {
		for _, p := range control.workers {
			stop(p)
		}
	}()
	waitReady := func(p *processWorker) error {
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("worker readiness timeout")
			case e := <-p.done:
				p.cmd = nil
				return fmt.Errorf("worker exited before ready: %v", e)
			case <-tick.C:
				control.mu.Lock()
				ok := !p.heartbeat.IsZero()
				control.mu.Unlock()
				if ok {
					return nil
				}
			}
		}
	}
	for i := range control.workers {
		p := &processWorker{}
		control.mu.Lock()
		control.workers[i] = p
		control.mu.Unlock()
		if e = spawn(i, p); e != nil {
			return e
		}
		if e = waitReady(p); e != nil {
			return e
		}
	}
	if ready != nil {
		ready <- listener.Addr().String()
		close(ready)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for i, p := range control.workers {
				if p.exhausted {
					continue
				}
				if p.cmd != nil {
					select {
					case <-p.done:
						p.cmd = nil
						p.crashes = append(p.crashes, time.Now())
						p.next = time.Now().Add(restartDelay(len(p.crashes)))
					default:
						continue
					}
				}
				kept := p.crashes[:0]
				for _, t := range p.crashes {
					if time.Since(t) < 5*time.Minute {
						kept = append(kept, t)
					}
				}
				p.crashes = kept
				if len(kept) > 5 {
					control.mu.Lock()
					p.exhausted = true
					control.mu.Unlock()
					continue
				}
				if time.Now().Before(p.next) {
					continue
				}
				if e = spawn(i, p); e != nil {
					return e
				}
			}
			if cfg.Reload {
				newHash, e := SourceHash(program, extra)
				if e != nil || newHash == hash {
					continue
				}
				candidateOpts := opts
				if loader != nil {
					candidateOpts.PlatformConfig, candidateOpts.PlatformGrants, e = loader()
					if e != nil {
						continue
					}
				}
				newContractHash, contractErr := contractSnapshot(opts.APIContractPath)
				newMaintenanceHash, maintenanceErr := workerMaintenanceSnapshot(cfg.WorkerFiles)
				newPolicy, _ := candidateOpts.PlatformConfig.Workers()
				// During overlap, keep pool policy and database manifests fixed.
				// Reject before Load can acquire any new-generation connections.
				compatible := contractErr == nil && newContractHash == contractHash && maintenanceErr == nil && newMaintenanceHash == maintenanceHash && reflect.DeepEqual(newPolicy, policy) && candidateOpts.PlatformConfig.RedisPoolBudget() == opts.PlatformConfig.RedisPoolBudget() && reflect.DeepEqual(candidateOpts.PlatformConfig.Telemetry(), opts.PlatformConfig.Telemetry())
				if !compatible || candidateOpts.PlatformConfig.PoolBudget(cfg.Workers, opts.DataGrants.PoolBudget()) != nil {
					continue
				}
				candidate, e := loadWithStatic(program, template, staticPrefix, staticRoot, candidateOpts, io.Discard)
				if e != nil {
					continue
				}
				newConfig := candidate.code.PlatformConfiguration()
				newGrants := candidate.code.PlatformCapabilities()
				stable, e := SourceHash(program, extra)
				if e != nil || stable != newHash {
					candidate.retire()
					continue
				}
				candidate.retire()
				opts.PlatformConfig = newConfig
				opts.PlatformGrants = newGrants
				env = restrictedWorkerEnv(opts, cfg)
				hash = newHash
				for i, p := range control.workers {
					stop(p)
					if e = spawn(i, p); e != nil {
						return e
					}
					if e = waitReady(p); e != nil {
						return e
					}
				}
			}
		}
	}
}
func contractSnapshot(path string) ([32]byte, error) {
	if path == "" {
		return sha256.Sum256(nil), nil
	}
	b, e := os.ReadFile(path)
	return sha256.Sum256(b), e
}
func workerMaintenanceSnapshot(paths []string) ([32]byte, error) {
	h := sha256.New()
	for _, path := range paths {
		if path == "" {
			continue
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return [32]byte{}, e
		}
		fmt.Fprintf(h, "%d:", len(b))
		h.Write(b)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}
func restartDelay(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > 5 {
		return 30 * time.Second
	}
	return time.Second * time.Duration(1<<uint(n-1))
}
func restrictedWorkerEnv(opts interpreter.Options, cfg Config) []string {
	names := map[string]bool{}
	for _, name := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		names[name] = true
	}
	for _, name := range opts.PlatformConfig.SecretNames() {
		names[name] = true
	}
	for _, name := range opts.DataGrants.SecretNames() {
		names[name] = true
	}
	var env []string
	for name := range names {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	sort.Strings(env)
	return env
}

// WorkerHeartbeat is private authenticated parent IPC. Parent loss cancels the
// child, so a killed supervisor cannot leave an indefinitely orphaned listener.
func workerHeartbeat(ctx context.Context, cancel context.CancelFunc, ready func() bool) {
	endpoint := os.Getenv("GRAB_WORKER_CONTROL")
	if endpoint == "" {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			ready()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 1}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/control?worker="+os.Getenv("GRAB_WORKER_ID")+"&ready="+strconv.FormatBool(ready()), nil)
		req.Header.Set("Authorization", "Bearer "+os.Getenv("GRAB_WORKER_TOKEN"))
		response, e := client.Do(req)
		if e == nil {
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 16))
			response.Body.Close()
			if response.StatusCode == 200 {
				last = time.Now()
				if string(raw) == "drain" {
					cancel()
					return
				}
			}
		}
		if time.Since(last) > 5*time.Second {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
