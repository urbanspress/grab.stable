package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"grab/internal/data"
	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/server"
	"grab/internal/template"
)

const usageText = "usage: grab run [--db-config <manifest.json>] [--allow-db <alias:read|read-write|migrate>] [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] [--allow-net <host[,host...]>] <file.grab>\n" +
	"       grab migrate --db-config <manifest.json> --allow-db <alias:migrate> <migrations.grab>\n" +
	"       grab serve [--addr 127.0.0.1:8080] [--allow-public] [--trusted-proxy <ip[,cidr...]>] [--static <[prefix=]dir>] [--reload] [--max-concurrency 64] [--max-queue 128] [--queue-timeout 1s] [--request-timeout 10s] [--max-body-bytes 1048576] [--shutdown-timeout 10s] [--log-format text|json] [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] [--allow-net <host[,host...]>] <file.grab> [template.grab.html]\n" +
	"       database flags for serve: --db-config <manifest.json> --allow-db <alias:read|read-write>\n" +
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
	case "run", "migrate":
		cfg, positional, ok := parseCommonFlags(arguments[1:], false)
		if !ok || len(positional) != 1 {
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		filePath := positional[0]
		if e := configureDatabase(&cfg, false); e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
		cfg.opts.Context = ctx
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
		if e := configureDatabase(&cfg, true); e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
		var tmplFile string
		if len(positional) == 2 {
			tmplFile = positional[1]
		}
		cfg.server.Diagnostics = cfg.diagMode
		if err := server.ServeWithConfig(ctx, cfg.addr, grabFile, tmplFile, cfg.opts, cfg.server, stdout, ready); err != nil {
			source, _ := os.ReadFile(grabFile)
			fmt.Fprintln(stderr, interpreter.FormatDiagnostic(string(source), err, cfg.diagMode))
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
		if err := server.CheckRouteTemplates(program, filePath); err != nil {
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
	dbConfig string
	dbAllow  []string
	addr     string
	diagMode interpreter.DiagnosticMode
	opts     interpreter.Options
	server   server.Config
}

func parseCommonFlags(args []string, allowAddr bool) (commonFlagConfig, []string, bool) {
	cfg := commonFlagConfig{
		addr:     server.DefaultAddr,
		diagMode: interpreter.ModeDeveloper,
		server:   server.DefaultConfig(),
	}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--db-config" || strings.HasPrefix(arg, "--db-config=") {
			val, next, ok := consumeFlagValue(args, i, "--db-config")
			if !ok || cfg.dbConfig != "" {
				return cfg, nil, false
			}
			cfg.dbConfig = val
			i = next
			continue
		}
		if arg == "--allow-db" || strings.HasPrefix(arg, "--allow-db=") {
			val, next, ok := consumeFlagValue(args, i, "--allow-db")
			if !ok {
				return cfg, nil, false
			}
			cfg.dbAllow = append(cfg.dbAllow, val)
			i = next
			continue
		}
		if allowAddr {
			if arg == "--allow-public" {
				cfg.server.AllowPublic = true
				continue
			}
			if arg == "--reload" {
				cfg.server.Reload = true
				continue
			}
			name := strings.SplitN(arg, "=", 2)[0]
			if strings.Contains("|--trusted-proxy|--static|--log-format|--max-concurrency|--max-queue|--queue-timeout|--request-timeout|--max-body-bytes|--shutdown-timeout|", "|"+name+"|") {
				val, next, ok := consumeFlagValue(args, i, name)
				if !ok {
					return cfg, nil, false
				}
				i = next
				var err error
				switch name {
				case "--trusted-proxy":
					cfg.server.TrustedProxy = val
				case "--static":
					cfg.server.Static = val
				case "--log-format":
					cfg.server.LogFormat = val
				case "--max-concurrency":
					cfg.server.MaxConcurrency, err = strconv.Atoi(val)
				case "--max-queue":
					cfg.server.MaxQueue, err = strconv.Atoi(val)
				case "--max-body-bytes":
					cfg.server.MaxBodyBytes, err = strconv.ParseInt(val, 10, 64)
				case "--queue-timeout":
					cfg.server.QueueTimeout, err = time.ParseDuration(val)
				case "--request-timeout":
					cfg.server.RequestTimeout, err = time.ParseDuration(val)
				case "--shutdown-timeout":
					cfg.server.ShutdownTimeout, err = time.ParseDuration(val)
				}
				if err != nil {
					return cfg, nil, false
				}
				continue
			}
		}
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
		if arg == "--allow-net" || strings.HasPrefix(arg, "--allow-net=") {
			val, nextIdx, ok := consumeFlagValue(args, i, "--allow-net")
			if !ok {
				return cfg, nil, false
			}
			hosts, valid := interpreter.ParseAllowNetList(val)
			if !valid {
				return cfg, nil, false
			}
			cfg.opts.Permissions.AllowNet = append(cfg.opts.Permissions.AllowNet, hosts...)
			i = nextIdx
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return cfg, nil, false
		}
		positional = append(positional, arg)
	}
	if allowAddr && (cfg.server.Validate() != nil || server.ValidateAddr(cfg.addr, true) != nil) {
		return cfg, nil, false
	}
	return cfg, positional, true
}

func configureDatabase(cfg *commonFlagConfig, serverMode bool) error {
	if cfg.dbConfig == "" {
		if len(cfg.dbAllow) != 0 {
			return fmt.Errorf("GRAB-E403: --allow-db requires --db-config")
		}
		return nil
	}
	path, e := filepath.Abs(cfg.dbConfig)
	if e != nil {
		return fmt.Errorf("GRAB-E210: invalid database configuration path")
	}
	f, e := os.Open(path)
	if e != nil {
		return fmt.Errorf("GRAB-E210: database configuration unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return fmt.Errorf("GRAB-E210: database configuration unavailable")
	}
	c, e := data.ParseConfig(raw, path)
	if e != nil {
		return e
	}
	cfg.opts.DataGrants, e = c.Grant(cfg.dbAllow, serverMode)
	return e
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
