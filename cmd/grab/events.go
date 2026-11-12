package main

import (
	"context"
	"encoding/json"
	"fmt"
	"grab/internal/connect"
	"grab/internal/data"
	"grab/internal/events"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func eventMaintenance(ctx context.Context, args []string, migrate bool, stdout, stderr io.Writer) int {
	action := "migrate"
	if !migrate {
		if len(args) == 0 {
			return 2
		}
		action = args[0]
		args = args[1:]
		if action != "inspect" && action != "retry" && action != "resolve" && action != "purge" {
			return 2
		}
	}
	values := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		name := strings.SplitN(args[i], "=", 2)[0]
		if name == "--store" || name == "--events-store" || name == "--id" || name == "--reason" || name == "--outcome" || name == "--before" {
			v, next, ok := consumeFlagValue(args, i, name)
			if !ok || values[name] != "" {
				return 2
			}
			values[name] = v
			i = next
		} else {
			rest = append(rest, args[i])
		}
	}
	alias := values["--store"]
	if migrate {
		alias = values["--events-store"]
	}
	if alias == "" || !migrate && action != "purge" && values["--id"] == "" {
		return 2
	}
	cfg, pos, ok := parseCommonFlags(rest, false)
	if !ok || len(pos) != 0 {
		return 2
	}
	e := configurePlatform(&cfg)
	if e == nil {
		e = configureDatabase(&cfg, false)
	}
	if e == nil {
		e = cfg.opts.PlatformGrants.Events(alias, "work")
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	def, ok := cfg.opts.PlatformConfig.Events()[alias]
	if !ok {
		return 1
	}
	manager := data.NewManager(cfg.opts.DataGrants, false)
	defer manager.Close()
	scope := manager.Scope()
	defer scope.Close()
	pool, e := scope.Open(ctx, def.Database)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	store := &events.Store{Driver: pool.Driver(), Config: def}
	if migrate {
		e = scope.MigrateEvents(ctx, pool, events.MigrationSteps(pool.Driver()))
		if e == nil {
			store.DB, e = scope.HostMaintenanceDatabase(pool)
		}
		if e == nil {
			e = store.InitNamespace(ctx)
		}
	} else {
		store.DB, e = scope.HostDatabase(pool)
		if e == nil {
			e = store.Ready(ctx)
		}
		if e == nil {
			if action == "purge" {
				before, err := strconv.ParseInt(values["--before"], 10, 64)
				if err != nil {
					return 2
				}
				var n int
				n, e = store.Purge(ctx, before, values["--reason"])
				if e == nil {
					fmt.Fprintln(stdout, n)
				}
			} else if action == "inspect" {
				var v events.Status
				v, e = store.Inspect(ctx, values["--id"])
				if e == nil {
					e = json.NewEncoder(stdout).Encode(v)
				}
			} else {
				e = store.Maintain(ctx, values["--id"], action, values["--outcome"], values["--reason"])
			}
		}
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	return 0
}
func connectorMaintenance(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "lock" && args[0] != "inspect" {
		return 2
	}
	cfg, pos, ok := parseCommonFlags(args[1:], false)
	if !ok || len(pos) != 0 || cfg.platformConfig == "" {
		return 2
	}
	if e := configurePlatform(&cfg); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	if args[0] == "inspect" {
		p, e := connect.Load(cfg.opts.PlatformConfig)
		if e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
		for _, id := range connect.IDs(p) {
			fmt.Fprintln(stdout, id)
		}
		return 0
	}
	b, e := connect.BuildLock(cfg.opts.PlatformConfig)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	path := filepath.Join(cfg.opts.PlatformConfig.Root(), "grab.connect.lock")
	if st, e := os.Lstat(path); e == nil && !st.Mode().IsRegular() {
		return 1
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".connect-lock-")
	if e == nil {
		tmp := f.Name()
		_, e = f.Write(append(b, '\n'))
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e == nil {
			e = os.Rename(tmp, path)
		}
		if e != nil {
			os.Remove(tmp)
		}
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	return 0
}
