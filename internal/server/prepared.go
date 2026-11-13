package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"grab/internal/ast"
	"grab/internal/interpreter"
	"grab/internal/parser"
	grabrt "grab/internal/runtime"
	"grab/internal/template"
)

// PreparedApp contains code and templates compiled once per successful load.
type PreparedApp struct {
	refsMu  sync.Mutex
	refs    int
	retired bool
	code    *interpreter.PreparedProgram
	legacy  *template.Template
	files   map[string]time.Time
	assets  *assetManifest
}

type assetManifest struct {
	prefix string
	hashes map[string]string
}

func buildAssetManifest(staticPrefix, staticRoot string, watchFiles map[string]time.Time) (*assetManifest, error) {
	if staticPrefix == "" || staticRoot == "" {
		return nil, nil
	}
	hashes := map[string]string{}
	err := filepath.WalkDir(staticRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != staticRoot && sensitive(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil || !isWithinDir(realPath, staticRoot) || sensitive(realPath) {
			return nil
		}
		rel, err := filepath.Rel(staticRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		urlRel := filepath.ToSlash(rel)
		if sensitive(urlRel) {
			return nil
		}
		f, err := os.Open(realPath)
		if err != nil {
			return nil
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			_ = f.Close()
			return nil
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		_ = f.Close()
		if copyErr != nil {
			return copyErr
		}
		hashes[staticPrefix+urlRel] = hex.EncodeToString(h.Sum(nil))[:12]
		if watchFiles != nil {
			watchFiles[path] = st.ModTime()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &assetManifest{prefix: staticPrefix, hashes: hashes}, nil
}

func (m *assetManifest) resolve(raw string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("static asset serving is not configured")
	}
	if raw == "" || strings.ContainsAny(raw, "\x00\\?#") || sensitive(raw) {
		return "", fmt.Errorf("invalid static asset path")
	}
	var rel string
	switch {
	case strings.HasPrefix(raw, m.prefix):
		rel = strings.TrimPrefix(raw, m.prefix)
	case strings.HasPrefix(raw, "/"):
		return "", fmt.Errorf("unknown static asset")
	default:
		rel = strings.TrimPrefix(raw, "./")
	}
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasSuffix(rel, "/") {
		return "", fmt.Errorf("invalid static asset path")
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." || sensitive(seg) {
			return "", fmt.Errorf("invalid static asset path")
		}
	}
	urlPath := m.prefix + rel
	hash, ok := m.hashes[urlPath]
	if !ok {
		return "", fmt.Errorf("unknown static asset")
	}
	return urlPath + "?v=" + hash, nil
}

func readProgram(path string) (*ast.Program, error) {
	if !strings.HasSuffix(path, ".grab") || strings.ContainsRune(path, '\x00') || (!filepath.IsAbs(path) && escapesRelativeRoot(path)) {
		return nil, fmt.Errorf("invalid .grab program path")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("program must be a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parser.Parse(path, string(b))
}

type templateCache struct {
	mu      sync.Mutex
	items   map[string]*template.Template
	program string
	files   map[string]time.Time
}

func (c *templateCache) load(path string) (*template.Template, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.items[path]; t != nil {
		return t, nil
	}
	resolved := path
	if !filepath.IsAbs(path) {
		resolved = filepath.Join(filepath.Dir(c.program), path)
	}
	_, resolved, err := ResolveFiles(c.program, resolved)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	t, err := template.Parse(resolved, string(b))
	if err != nil {
		return nil, err
	}
	c.items[path] = t
	if st, e := os.Stat(resolved); e == nil {
		c.files[resolved] = st.ModTime()
	}
	return t, nil
}
func (c *templateCache) render(path string, data map[string]grabrt.Value) (string, error) {
	t, e := c.load(path)
	if e != nil {
		return "", e
	}
	return template.Render(t, data)
}

// CheckRouteTemplates performs static template checks without startup side effects.
func CheckRouteTemplates(program *ast.Program, path string) error {
	c := &templateCache{items: map[string]*template.Template{}, program: path, files: map[string]time.Time{}}
	return checkTemplates(program, c)
}
func checkTemplates(program *ast.Program, c *templateCache) error {
	paths := interpreter.LiteralTemplates(program)
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(i, j int) bool { return paths[ordered[i]].Start.Offset < paths[ordered[j]].Start.Offset })
	for _, path := range ordered {
		span := paths[path]
		if _, e := c.load(path); e != nil {
			return &interpreter.Error{Kind: interpreter.InvalidRouteDefinition, Span: span, Err: e}
		}
	}
	return nil
}

func Load(grabPath, templatePath string, opts interpreter.Options, output io.Writer) (*PreparedApp, error) {
	return loadWithStatic(grabPath, templatePath, "", "", opts, output)
}

func loadWithStatic(grabPath, templatePath, staticPrefix, staticRoot string, opts interpreter.Options, output io.Writer) (*PreparedApp, error) {
	if opts.PlatformLoader != nil {
		var e error
		opts.PlatformConfig, opts.PlatformGrants, e = opts.PlatformLoader()
		if e != nil {
			return nil, e
		}
	}
	program, err := readProgram(grabPath)
	if err != nil {
		return nil, err
	}
	bound, err := interpreter.Validate(program)
	if err != nil {
		return nil, err
	}
	c := &templateCache{items: map[string]*template.Template{}, program: grabPath, files: map[string]time.Time{}}
	if err = checkTemplates(program, c); err != nil {
		return nil, err
	}
	assets, err := buildAssetManifest(staticPrefix, staticRoot, c.files)
	if err != nil {
		return nil, err
	}
	if assets != nil {
		opts.AssetResolver = assets.resolve
	}
	opts.TemplateLoader = c.render
	p, err := interpreter.Prepare(program, opts, output)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = p.Close()
		}
	}()
	app := &PreparedApp{code: p, files: c.files, assets: assets}
	if opts.APIContractPath != "" {
		st, e := os.Stat(opts.APIContractPath)
		if e != nil {
			return nil, e
		}
		app.files[opts.APIContractPath] = st.ModTime()
	}
	if opts.PlatformPath != "" {
		st, e := os.Stat(opts.PlatformPath)
		if e != nil {
			return nil, e
		}
		app.files[opts.PlatformPath] = st.ModTime()
	}
	if len(p.Routes()) == 0 && len(opts.PlatformConfig.Channels()) == 0 {
		_, tp, e := ResolveFiles(grabPath, templatePath)
		if e != nil {
			return nil, e
		}
		b, e := os.ReadFile(tp)
		if e != nil {
			return nil, e
		}
		app.legacy, e = template.Parse(tp, string(b))
		if e != nil {
			return nil, e
		}
		if e = template.ValidateBindings(app.legacy, bound); e != nil {
			return nil, e
		}
		if st, e := os.Stat(tp); e == nil {
			app.files[tp] = st.ModTime()
		}
	}
	if st, e := os.Stat(grabPath); e == nil {
		app.files[grabPath] = st.ModTime()
	}
	// Copy dependencies: dynamic templates must not mutate the reload snapshot.
	files := map[string]time.Time{}
	for p, t := range app.files {
		files[p] = t
	}
	app.files = files
	if !opts.DeferWorkers {
		p.StartWorkers()
	}
	success = true
	return app, nil
}

func (p *PreparedApp) acquire() bool {
	p.refsMu.Lock()
	defer p.refsMu.Unlock()
	if p.retired {
		return false
	}
	p.refs++
	return true
}
func (p *PreparedApp) release() {
	p.refsMu.Lock()
	p.refs--
	closeNow := p.retired && p.refs == 0
	p.refsMu.Unlock()
	if closeNow {
		_ = p.code.Close()
	}
}
func (p *PreparedApp) retire() {
	p.code.StopWorkers()
	p.code.StopSockets()
	p.refsMu.Lock()
	p.retired = true
	closeNow := p.refs == 0
	p.refsMu.Unlock()
	if closeNow {
		_ = p.code.Close()
	}
}

func (p *PreparedApp) execute(ctx context.Context, route *interpreter.Route, req grabrt.Value, output io.Writer) (interpreter.Response, error) {
	if p.legacy != nil {
		_, env, e := p.code.Execute(ctx, route, req, output)
		if e != nil {
			return interpreter.Response{}, e
		}
		b, e := template.Render(p.legacy, env)
		return interpreter.Response{Status: 200, Headers: map[string][]string{"Content-Type": {"text/html; charset=utf-8"}}, Body: []byte(b)}, e
	}
	v, e := p.code.ExecuteRoute(ctx, route, req, output)
	if e != nil {
		return interpreter.Response{}, e
	}
	r, e := interpreter.DecodeResponse(v)
	if e != nil {
		e = &interpreter.Error{Kind: interpreter.InvalidServerResponse, Span: route.Definition.Span, Err: e}
	}
	if route != nil {
		if o, ok := p.code.ContractOperation(route); ok {
			if e != nil || p.code.ValidateWireResponse(o, r.Status, r.Body) != nil {
				return interpreter.Response{}, fmt.Errorf("GRAB-E315: invalid contracted response")
			}
		}
	}
	return r, e
}
