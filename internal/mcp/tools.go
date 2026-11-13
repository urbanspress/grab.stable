package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"grab/internal/ast"
	"grab/internal/contracts"
	"grab/internal/data"
	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/platform"
	"grab/internal/template"
)

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func toolDefinitions() []toolDef {
	return []toolDef{
		{
			Name:        "grab_check",
			Description: "Statically validate a .grab program and optional .grab.html templates inside the workspace without executing code.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"file"},
				"properties": map[string]any{
					"file":            map[string]any{"type": "string", "description": "Workspace-relative .grab file path"},
					"template":        map[string]any{"type": "string", "description": "Optional workspace-relative .grab.html template path"},
					"mode":            map[string]any{"type": "string", "enum": []string{"beginner", "developer"}, "description": "Diagnostic detail mode"},
					"db_config":       map[string]any{"type": "string", "description": "Optional workspace-relative grab.data.json path"},
					"platform_config": map[string]any{"type": "string", "description": "Optional workspace-relative grab.platform.json path"},
					"api_contract":    map[string]any{"type": "string", "description": "Optional workspace-relative grab.api.json path"},
				},
			},
		},
		{
			Name:        "grab_format",
			Description: "Format a .grab file or inline GRAB source canonically (2-space indentation). Writing to disk requires --allow-write.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"file":   map[string]any{"type": "string", "description": "Workspace-relative .grab file path"},
					"source": map[string]any{"type": "string", "description": "Inline .grab source text (read-only)"},
					"write":  map[string]any{"type": "boolean", "description": "Write formatted output back to file (requires --allow-write)"},
				},
			},
		},
		{
			Name:        "grab_inspect_routes",
			Description: "Inspect declared HTTP routes, parameters, imports, and referenced templates in a .grab file without executing code.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"file"},
				"properties": map[string]any{
					"file": map[string]any{"type": "string", "description": "Workspace-relative .grab file path"},
				},
			},
		},
		{
			Name:        "grab_inspect_schema",
			Description: "Inspect database, API contract, and platform manifests inside the workspace without resolving host secret values.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"db_config":       map[string]any{"type": "string", "description": "Optional workspace-relative grab.data.json path"},
					"api_contract":    map[string]any{"type": "string", "description": "Optional workspace-relative grab.api.json path"},
					"platform_config": map[string]any{"type": "string", "description": "Optional workspace-relative grab.platform.json path"},
				},
			},
		},
		{
			Name:        "grab_scaffold",
			Description: "Generate validated, canonically formatted GRAB starter files (.grab, .grab.html, or grab.api.json). Writing requires --allow-write unless dry_run is true.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"kind", "target"},
				"properties": map[string]any{
					"kind":      map[string]any{"type": "string", "enum": []string{"route", "template", "app", "api_contract"}},
					"target":    map[string]any{"type": "string", "description": "Workspace-relative destination path"},
					"name":      map[string]any{"type": "string", "description": "Title or operation identifier"},
					"dry_run":   map[string]any{"type": "boolean", "description": "Return generated content without writing to disk"},
					"overwrite": map[string]any{"type": "boolean", "description": "Allow replacing an existing target file"},
					"routes": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"method":   map[string]any{"type": "string"},
								"path":     map[string]any{"type": "string"},
								"template": map[string]any{"type": "string"},
							},
						},
					},
					"fields": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
								"type": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
}

func decodeToolArgs(raw json.RawMessage, dst any) *rpcError {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &rpcError{Code: -32602, Message: "invalid tool arguments"}
	}
	if dec.More() {
		return &rpcError{Code: -32602, Message: "trailing tool argument data"}
	}
	return nil
}

func toolJSONResult(payload any, isErr bool) (map[string]any, *rpcError) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: "failed to encode tool response"}
	}
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(b),
			},
		},
		"isError": isErr,
	}, nil
}

func toolErrorResult(err error) (map[string]any, *rpcError) {
	message := err.Error()
	code, category, _, _ := interpreter.ExtractErrorMeta(err)
	if strings.Contains(message, "GRAB-E401") {
		code = "GRAB-E401"
		category = "capability"
	} else if strings.Contains(message, "GRAB-E403") {
		code = "GRAB-E403"
		category = "capability"
	} else if strings.Contains(message, "GRAB-E210") {
		code = "GRAB-E210"
		category = "validation"
	}
	return toolJSONResult(map[string]any{
		"error": map[string]any{
			"code":     code,
			"category": category,
			"message":  message,
		},
	}, true)
}

func (s *Server) dispatchTool(name string, rawArgs json.RawMessage) (map[string]any, *rpcError) {
	switch name {
	case "grab_check":
		return s.callGrabCheck(rawArgs)
	case "grab_format":
		return s.callGrabFormat(rawArgs)
	case "grab_inspect_routes":
		return s.callGrabInspectRoutes(rawArgs)
	case "grab_inspect_schema":
		return s.callGrabInspectSchema(rawArgs)
	case "grab_scaffold":
		return s.callGrabScaffold(rawArgs)
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + name}
	}
}

type diagnosticEntry struct {
	Code      string `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Formatted string `json:"formatted"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
}

func makeDiagnostic(source, file string, err error, mode interpreter.DiagnosticMode) diagnosticEntry {
	code, cat, span, _ := interpreter.ExtractErrorMeta(err)
	if code == "" {
		code = "GRAB-E201"
		cat = "validation"
	}
	return diagnosticEntry{
		Code:      code,
		Category:  cat,
		Message:   err.Error(),
		Formatted: interpreter.FormatDiagnostic(source, err, mode),
		File:      file,
		Line:      span.Start.Line,
		Column:    span.Start.Column,
	}
}

func (s *Server) callGrabCheck(rawArgs json.RawMessage) (map[string]any, *rpcError) {
	var args struct {
		File           string `json:"file"`
		Template       string `json:"template"`
		Mode           string `json:"mode"`
		DBConfig       string `json:"db_config"`
		PlatformConfig string `json:"platform_config"`
		APIContract    string `json:"api_contract"`
	}
	if rpcErr := decodeToolArgs(rawArgs, &args); rpcErr != nil {
		return nil, rpcErr
	}
	if strings.TrimSpace(args.File) == "" {
		return nil, &rpcError{Code: -32602, Message: "file is required"}
	}
	diagMode := interpreter.ModeDeveloper
	switch strings.TrimSpace(args.Mode) {
	case "", "developer":
		diagMode = interpreter.ModeDeveloper
	case "beginner":
		diagMode = interpreter.ModeBeginner
	default:
		return nil, &rpcError{Code: -32602, Message: "mode must be beginner or developer"}
	}

	absFile, relFile, err := s.resolveWorkspaceFile(args.File, false, ".grab")
	if err != nil {
		return toolErrorResult(err)
	}
	sourceBytes, err := readBoundedFile(absFile)
	if err != nil {
		return toolErrorResult(err)
	}
	source := string(sourceBytes)

	program, err := parser.Parse(relFile, source)
	if err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      0,
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}

	bound, err := interpreter.Validate(program)
	if err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      countRoutes(program),
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}

	var platCfg *platform.Config
	platPath := firstNonEmpty(args.PlatformConfig, s.cfg.PlatformConfig)
	if platPath != "" {
		absPlat, _, err := s.resolveWorkspaceFile(platPath, false, ".json")
		if err != nil {
			return toolErrorResult(err)
		}
		rawPlat, err := readBoundedFile(absPlat)
		if err != nil {
			return toolErrorResult(err)
		}
		platCfg, err = platform.Parse(rawPlat, absPlat)
		if err != nil {
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relFile,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
			}, false)
		}
	}
	if err := interpreter.ValidatePlatform(program, platCfg); err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      countRoutes(program),
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}

	var apiDoc *contracts.Document
	apiPath := firstNonEmpty(args.APIContract, s.cfg.APIContract)
	if apiPath != "" {
		absAPI, _, err := s.resolveWorkspaceFile(apiPath, false, ".json")
		if err != nil {
			return toolErrorResult(err)
		}
		rawAPI, err := readBoundedFile(absAPI)
		if err != nil {
			return toolErrorResult(err)
		}
		apiDoc, err = contracts.Parse(rawAPI)
		if err != nil {
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relFile,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
			}, false)
		}
	}
	if _, err := interpreter.ValidateIntegrations(program, platCfg); err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      countRoutes(program),
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}
	if err := interpreter.ValidateContracts(program, apiDoc); err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      countRoutes(program),
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}
	if err := interpreter.ValidateSockets(program, platCfg, apiDoc); err != nil {
		return toolJSONResult(map[string]any{
			"ok":          false,
			"file":        relFile,
			"routes":      countRoutes(program),
			"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, err, diagMode)},
		}, false)
	}

	// Validate literal server.html template paths strictly inside workspaceRoot.
	litTemplates := interpreter.LiteralTemplates(program)
	orderedTemplates := make([]string, 0, len(litTemplates))
	for tp := range litTemplates {
		orderedTemplates = append(orderedTemplates, tp)
	}
	sort.Slice(orderedTemplates, func(i, j int) bool {
		return litTemplates[orderedTemplates[i]].Start.Offset < litTemplates[orderedTemplates[j]].Start.Offset
	})
	programDir := filepath.Dir(absFile)
	for _, tp := range orderedTemplates {
		candidate := tp
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(programDir, candidate)
		}
		absTmpl, relTmpl, err := s.resolveWorkspaceFile(candidate, false, ".grab.html")
		if err != nil {
			wrapped := &interpreter.Error{Kind: interpreter.InvalidRouteDefinition, Span: litTemplates[tp], Err: err}
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relFile,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, wrapped, diagMode)},
			}, false)
		}
		tmplBytes, err := readBoundedFile(absTmpl)
		if err != nil {
			return toolErrorResult(err)
		}
		if _, err := template.Parse(relTmpl, string(tmplBytes)); err != nil {
			wrapped := &interpreter.Error{Kind: interpreter.InvalidRouteDefinition, Span: litTemplates[tp], Err: err}
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relFile,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(source, relFile, wrapped, diagMode)},
			}, false)
		}
	}

	if strings.TrimSpace(args.Template) != "" {
		absTmpl, relTmpl, err := s.resolveWorkspaceFile(args.Template, false, ".grab.html")
		if err != nil {
			return toolErrorResult(err)
		}
		tmplBytes, err := readBoundedFile(absTmpl)
		if err != nil {
			return toolErrorResult(err)
		}
		tmpl, err := template.Parse(relTmpl, string(tmplBytes))
		if err != nil {
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relTmpl,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(string(tmplBytes), relTmpl, err, diagMode)},
			}, false)
		}
		if err := template.ValidateBindings(tmpl, bound); err != nil {
			return toolJSONResult(map[string]any{
				"ok":          false,
				"file":        relTmpl,
				"routes":      countRoutes(program),
				"diagnostics": []diagnosticEntry{makeDiagnostic(string(tmplBytes), relTmpl, err, diagMode)},
			}, false)
		}
	}

	return toolJSONResult(map[string]any{
		"ok":          true,
		"file":        relFile,
		"routes":      countRoutes(program),
		"diagnostics": []diagnosticEntry{},
	}, false)
}

func countRoutes(p *ast.Program) int {
	n := 0
	for _, stmt := range p.Statements {
		if _, ok := stmt.(*ast.RouteDef); ok {
			n++
		}
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func (s *Server) callGrabFormat(rawArgs json.RawMessage) (map[string]any, *rpcError) {
	var args struct {
		File   string `json:"file"`
		Source string `json:"source"`
		Write  bool   `json:"write"`
	}
	if rpcErr := decodeToolArgs(rawArgs, &args); rpcErr != nil {
		return nil, rpcErr
	}
	hasFile := strings.TrimSpace(args.File) != ""
	hasSource := args.Source != ""
	if hasFile == hasSource {
		return nil, &rpcError{Code: -32602, Message: "provide exactly one of file or source"}
	}
	if hasSource {
		if args.Write {
			return nil, &rpcError{Code: -32602, Message: "write requires file, not source"}
		}
		formatted, err := parser.Format("<input.grab>", args.Source)
		if err != nil {
			return toolErrorResult(err)
		}
		return toolJSONResult(map[string]any{
			"formatted": formatted,
			"changed":   formatted != args.Source,
			"written":   false,
		}, false)
	}

	if args.Write && !s.cfg.AllowWrite {
		return toolErrorResult(fmt.Errorf("GRAB-E401: write access denied; start grab mcp with --allow-write"))
	}
	absFile, relFile, err := s.resolveWorkspaceFile(args.File, false, ".grab")
	if err != nil {
		return toolErrorResult(err)
	}
	rawBytes, err := readBoundedFile(absFile)
	if err != nil {
		return toolErrorResult(err)
	}
	orig := string(rawBytes)
	formatted, err := parser.Format(relFile, orig)
	if err != nil {
		return toolErrorResult(err)
	}
	changed := formatted != orig
	if args.Write && changed {
		if err := os.WriteFile(absFile, []byte(formatted), 0o644); err != nil {
			return toolErrorResult(err)
		}
	}
	return toolJSONResult(map[string]any{
		"file":      relFile,
		"formatted": formatted,
		"changed":   changed,
		"written":   args.Write,
	}, false)
}

func (s *Server) callGrabInspectRoutes(rawArgs json.RawMessage) (map[string]any, *rpcError) {
	var args struct {
		File string `json:"file"`
	}
	if rpcErr := decodeToolArgs(rawArgs, &args); rpcErr != nil {
		return nil, rpcErr
	}
	if strings.TrimSpace(args.File) == "" {
		return nil, &rpcError{Code: -32602, Message: "file is required"}
	}
	absFile, relFile, err := s.resolveWorkspaceFile(args.File, false, ".grab")
	if err != nil {
		return toolErrorResult(err)
	}
	rawBytes, err := readBoundedFile(absFile)
	if err != nil {
		return toolErrorResult(err)
	}
	program, err := parser.Parse(relFile, string(rawBytes))
	if err != nil {
		return toolErrorResult(err)
	}

	type routeInfo struct {
		Method        string   `json:"method"`
		Path          string   `json:"path"`
		Params        []string `json:"params"`
		HasReqBinding bool     `json:"has_req_binding"`
		ReqName       string   `json:"req_name,omitempty"`
		Line          int      `json:"line"`
		Column        int      `json:"column"`
	}

	var uses []string
	var routes []routeInfo
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.UseDecl:
			uses = append(uses, node.Module)
		case *ast.RouteDef:
			var params []string
			for _, seg := range strings.Split(node.Path, "/") {
				if len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}' {
					params = append(params, seg[1:len(seg)-1])
				}
			}
			routes = append(routes, routeInfo{
				Method:        node.Method,
				Path:          node.Path,
				Params:        params,
				HasReqBinding: node.RequestName != "",
				ReqName:       node.RequestName,
				Line:          node.Span.Start.Line,
				Column:        node.Span.Start.Column,
			})
		}
	}
	// Sort using GRAB's deterministic static-before-parameter priority order.
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := strings.Split(routes[i].Path, "/"), strings.Split(routes[j].Path, "/")
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		for k := 0; k < len(a) && k < len(b); k++ {
			ap, bp := strings.HasPrefix(a[k], "{"), strings.HasPrefix(b[k], "{")
			if ap != bp {
				return !ap
			}
		}
		return false
	})

	litTemplates := interpreter.LiteralTemplates(program)
	templates := make([]string, 0, len(litTemplates))
	for tp := range litTemplates {
		templates = append(templates, tp)
	}
	sort.Strings(templates)

	return toolJSONResult(map[string]any{
		"file":      relFile,
		"uses":      uses,
		"routes":    routes,
		"templates": templates,
	}, false)
}

func (s *Server) callGrabInspectSchema(rawArgs json.RawMessage) (map[string]any, *rpcError) {
	var args struct {
		DBConfig       string `json:"db_config"`
		APIContract    string `json:"api_contract"`
		PlatformConfig string `json:"platform_config"`
	}
	if rpcErr := decodeToolArgs(rawArgs, &args); rpcErr != nil {
		return nil, rpcErr
	}

	out := map[string]any{}

	resolveOptional := func(explicit, configured, defaultName string) (abs, rel string, found bool, err error) {
		chosen := firstNonEmpty(explicit, configured)
		if chosen != "" {
			a, r, e := s.resolveWorkspaceFile(chosen, false, ".json")
			return a, r, true, e
		}
		candidate := filepath.Join(s.workspaceRoot, defaultName)
		if _, stErr := os.Stat(candidate); stErr == nil {
			a, r, e := s.resolveWorkspaceFile(defaultName, false, ".json")
			return a, r, true, e
		}
		return "", "", false, nil
	}

	if absDB, relDB, found, err := resolveOptional(args.DBConfig, s.cfg.DBConfig, "grab.data.json"); err != nil {
		return toolErrorResult(err)
	} else if found {
		rawDB, err := readBoundedFile(absDB)
		if err != nil {
			return toolErrorResult(err)
		}
		cfg, err := data.ParseConfig(rawDB, absDB)
		if err != nil {
			return toolErrorResult(err)
		}
		summaries := cfg.Summaries()
		// Redact absolute host path prefixes in summaries to workspace-relative when possible.
		for alias, sum := range summaries {
			if sum.Path != "" && sum.Path != ":memory:" {
				if relP, relErr := filepath.Rel(s.workspaceRoot, sum.Path); relErr == nil && !strings.HasPrefix(relP, "..") {
					sum.Path = filepath.ToSlash(relP)
				} else {
					sum.Path = filepath.Base(sum.Path)
				}
				summaries[alias] = sum
			}
		}
		out["database"] = map[string]any{
			"file":        relDB,
			"connections": summaries,
		}
	}

	if absAPI, relAPI, found, err := resolveOptional(args.APIContract, s.cfg.APIContract, "grab.api.json"); err != nil {
		return toolErrorResult(err)
	} else if found {
		rawAPI, err := readBoundedFile(absAPI)
		if err != nil {
			return toolErrorResult(err)
		}
		doc, err := contracts.Parse(rawAPI)
		if err != nil {
			return toolErrorResult(err)
		}
		out["api_contract"] = map[string]any{
			"file":       relAPI,
			"version":    doc.Version,
			"types":      doc.Types,
			"operations": doc.Operations,
		}
	}

	if absPlat, relPlat, found, err := resolveOptional(args.PlatformConfig, s.cfg.PlatformConfig, "grab.platform.json"); err != nil {
		return toolErrorResult(err)
	} else if found {
		rawPlat, err := readBoundedFile(absPlat)
		if err != nil {
			return toolErrorResult(err)
		}
		pcfg, err := platform.Parse(rawPlat, absPlat)
		if err != nil {
			return toolErrorResult(err)
		}
		caches := map[string]map[string]any{}
		for alias, c := range pcfg.Caches() {
			caches[alias] = map[string]any{
				"driver":       c.Driver,
				"namespace":    c.Namespace,
				"max_keys":     c.MaxKeys,
				"password_env": c.PasswordEnv,
			}
		}
		channels := map[string]map[string]any{}
		for alias, ch := range pcfg.Channels() {
			channels[alias] = map[string]any{
				"path":        ch.Path,
				"driver":      ch.Broker.Driver,
				"client_type": ch.ClientType,
				"server_type": ch.ServerType,
			}
		}
		connectors := map[string]map[string]any{}
		for alias, cn := range pcfg.Connectors() {
			connectors[alias] = map[string]any{
				"package":     cn.Package,
				"version":     cn.Version,
				"secret_envs": cn.Secrets,
			}
		}
		events := map[string]map[string]any{}
		for alias, ev := range pcfg.Events() {
			events[alias] = map[string]any{
				"database": ev.Database,
			}
		}
		out["platform"] = map[string]any{
			"file":       relPlat,
			"caches":     caches,
			"channels":   channels,
			"connectors": connectors,
			"events":     events,
		}
	}

	return toolJSONResult(out, false)
}

func (s *Server) callGrabScaffold(rawArgs json.RawMessage) (map[string]any, *rpcError) {
	var args struct {
		Kind      string `json:"kind"`
		Target    string `json:"target"`
		Name      string `json:"name"`
		DryRun    bool   `json:"dry_run"`
		Overwrite bool   `json:"overwrite"`
		Routes    []struct {
			Method   string `json:"method"`
			Path     string `json:"path"`
			Template string `json:"template"`
		} `json:"routes"`
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if rpcErr := decodeToolArgs(rawArgs, &args); rpcErr != nil {
		return nil, rpcErr
	}
	if !args.DryRun && !s.cfg.AllowWrite {
		return toolErrorResult(fmt.Errorf("GRAB-E401: write access denied; start grab mcp with --allow-write"))
	}

	var expectedExt string
	switch args.Kind {
	case "app", "route":
		expectedExt = ".grab"
	case "template":
		expectedExt = ".grab.html"
	case "api_contract":
		expectedExt = ".json"
	default:
		return nil, &rpcError{Code: -32602, Message: "kind must be route, template, app, or api_contract"}
	}

	absTarget, relTarget, err := s.resolveWorkspaceFile(args.Target, true, expectedExt)
	if err != nil {
		return toolErrorResult(err)
	}
	if !args.DryRun && !args.Overwrite {
		if _, stErr := os.Stat(absTarget); stErr == nil {
			return toolErrorResult(fmt.Errorf("target %q already exists (pass overwrite: true to replace)", relTarget))
		}
	}

	var content string
	switch args.Kind {
	case "app", "route":
		var b strings.Builder
		b.WriteString("use server\n\n")
		if len(args.Routes) == 0 {
			title := strings.TrimSpace(args.Name)
			if title == "" {
				title = "GRAB App"
			}
			b.WriteString("route \"GET\" \"/\"\n")
			b.WriteString(fmt.Sprintf("  give back server.json using 200, record {message: %q}\n", title))
			b.WriteString("end\n")
		} else {
			for i, r := range args.Routes {
				if i > 0 {
					b.WriteString("\n")
				}
				method := strings.ToUpper(strings.TrimSpace(r.Method))
				if method == "" {
					method = "GET"
				}
				path := strings.TrimSpace(r.Path)
				if path == "" {
					path = "/"
				}
				b.WriteString(fmt.Sprintf("route %q %q\n", method, path))
				if tmpl := strings.TrimSpace(r.Template); tmpl != "" {
					b.WriteString(fmt.Sprintf("  give back server.html using %q, record {title: %q}\n", tmpl, firstNonEmpty(args.Name, "Page")))
				} else {
					b.WriteString(fmt.Sprintf("  give back server.json using 200, record {ok: yes, path: %q}\n", path))
				}
				b.WriteString("end\n")
			}
		}
		formatted, err := parser.Format(relTarget, b.String())
		if err != nil {
			return toolErrorResult(err)
		}
		prog, err := parser.Parse(relTarget, formatted)
		if err != nil {
			return toolErrorResult(err)
		}
		if _, err := interpreter.Validate(prog); err != nil {
			return toolErrorResult(err)
		}
		content = formatted

	case "template":
		title := firstNonEmpty(args.Name, "title")
		var b strings.Builder
		b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>{")
		if len(args.Fields) > 0 && isSafeIdent(args.Fields[0].Name) {
			b.WriteString(args.Fields[0].Name)
		} else if isSafeIdent(title) {
			b.WriteString(title)
		} else {
			b.WriteString("title")
		}
		b.WriteString("}</title>\n</head>\n<body>\n<main>\n")
		if len(args.Fields) == 0 {
			b.WriteString("  <h1>{title}</h1>\n")
		} else {
			for _, f := range args.Fields {
				if !isSafeIdent(f.Name) {
					return nil, &rpcError{Code: -32602, Message: "invalid template field identifier: " + f.Name}
				}
				b.WriteString(fmt.Sprintf("  <p>{%s}</p>\n", f.Name))
			}
		}
		b.WriteString("</main>\n</body>\n</html>\n")
		content = b.String()
		if _, err := template.Parse(relTarget, content); err != nil {
			return toolErrorResult(err)
		}

	case "api_contract":
		opID := firstNonEmpty(args.Name, "get_status")
		if !isSafeIdent(opID) {
			return nil, &rpcError{Code: -32602, Message: "invalid operation identifier: " + opID}
		}
		var fields []contracts.Field
		if len(args.Fields) == 0 {
			fields = []contracts.Field{{Name: "ok", Schema: contracts.Schema{Type: "boolean"}}}
		} else {
			for _, f := range args.Fields {
				if !isSafeIdent(f.Name) {
					return nil, &rpcError{Code: -32602, Message: "invalid contract field identifier: " + f.Name}
				}
				ft := firstNonEmpty(f.Type, "text")
				fields = append(fields, contracts.Field{Name: f.Name, Schema: contracts.Schema{Type: ft}})
			}
		}
		doc := contracts.Document{
			Version: 1,
			Types:   map[string]contracts.Schema{},
			Operations: []contracts.Operation{
				{
					ID:     opID,
					Method: "GET",
					Path:   "/",
					Success: []contracts.Success{
						{
							Status: 200,
							Schema: contracts.Schema{Type: "record", Fields: fields},
						},
					},
				},
			},
		}
		rawDoc, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return toolErrorResult(err)
		}
		content = string(rawDoc) + "\n"
		if _, err := contracts.Parse([]byte(content)); err != nil {
			return toolErrorResult(err)
		}
	}

	if !args.DryRun {
		parent := filepath.Dir(absTarget)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return toolErrorResult(err)
		}
		// Re-verify symlink confinement after parent creation.
		realParent, err := filepath.EvalSymlinks(parent)
		if err != nil || !interpreter.IsPathWithinRoot(realParent, s.workspaceRoot) {
			return toolErrorResult(fmt.Errorf("GRAB-E401: target directory escapes workspace root"))
		}
		if err := os.WriteFile(absTarget, []byte(content), 0o644); err != nil {
			return toolErrorResult(err)
		}
	}

	return toolJSONResult(map[string]any{
		"kind":    args.Kind,
		"target":  relTarget,
		"written": !args.DryRun,
		"content": content,
	}, false)
}

func isSafeIdent(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		if i == 0 && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_') {
			return false
		}
		if i > 0 && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}
