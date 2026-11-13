package mcp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"grab/internal/interpreter"
)

const maxReadBytes = 10 * 1024 * 1024 // 10 MiB

// Config configures the native GRAB MCP server.
type Config struct {
	Workspace      string
	AllowWrite     bool
	DBConfig       string
	PlatformConfig string
	APIContract    string
}

// Server implements GRAB's workspace-confined stdio MCP server.
type Server struct {
	cfg           Config
	workspaceRoot string
}

// NewServer creates a workspace-confined MCP server. If cfg.Workspace is empty,
// the current working directory is used and canonicalized via EvalSymlinks.
func NewServer(cfg Config) (*Server, error) {
	ws := strings.TrimSpace(cfg.Workspace)
	if ws == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("GRAB-E401: unable to resolve workspace: %w", err)
		}
		ws = wd
	}
	absWS, err := filepath.Abs(ws)
	if err != nil {
		return nil, fmt.Errorf("GRAB-E401: invalid workspace path: %w", err)
	}
	realWS, err := filepath.EvalSymlinks(absWS)
	if err != nil {
		return nil, fmt.Errorf("GRAB-E401: workspace does not exist: %w", err)
	}
	st, err := os.Stat(realWS)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("GRAB-E401: workspace must be an existing directory")
	}
	s := &Server{cfg: cfg, workspaceRoot: realWS}
	// Validate optional default manifest paths up front so they cannot escape.
	for _, m := range []string{cfg.DBConfig, cfg.PlatformConfig, cfg.APIContract} {
		if strings.TrimSpace(m) != "" {
			if _, _, err := s.resolveWorkspaceFile(m, true, ".json"); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// WorkspaceRoot returns the canonicalized workspace directory path.
func (s *Server) WorkspaceRoot() string {
	return s.workspaceRoot
}

func (s *Server) resolveWorkspaceFile(raw string, allowMissing bool, allowedExts ...string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsRune(raw, '\x00') {
		return "", "", fmt.Errorf("GRAB-E401: invalid empty or null path")
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return "", "", fmt.Errorf("GRAB-E401: UNC or network paths are not permitted")
	}
	if interpreter.HasBlockedSegments(raw) {
		return "", "", fmt.Errorf("GRAB-E401: access to sensitive or parent-traversal path %q is denied", raw)
	}

	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.workspaceRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("GRAB-E401: invalid path %q", raw)
	}
	realTarget, err := interpreter.EvalExistingSymlinks(absCandidate)
	if err != nil {
		return "", "", fmt.Errorf("GRAB-E401: path %q cannot be resolved", raw)
	}
	if !interpreter.IsPathWithinRoot(realTarget, s.workspaceRoot) || realTarget == s.workspaceRoot {
		return "", "", fmt.Errorf("GRAB-E401: path %q escapes workspace root", raw)
	}

	rel, err := filepath.Rel(s.workspaceRoot, realTarget)
	if err != nil || rel == "." {
		return "", "", fmt.Errorf("GRAB-E401: invalid workspace relative path %q", raw)
	}
	slashRel := filepath.ToSlash(rel)

	if len(allowedExts) > 0 {
		extOK := false
		lower := strings.ToLower(slashRel)
		for _, ext := range allowedExts {
			if strings.HasSuffix(lower, ext) {
				extOK = true
				break
			}
		}
		if !extOK {
			return "", "", fmt.Errorf("GRAB-E401: unsupported file extension for %q", slashRel)
		}
	}

	st, statErr := os.Stat(realTarget)
	if statErr != nil {
		if !allowMissing || !os.IsNotExist(statErr) {
			return "", "", fmt.Errorf("file %q not accessible: %w", slashRel, statErr)
		}
	} else {
		if !st.Mode().IsRegular() {
			return "", "", fmt.Errorf("GRAB-E401: %q is not a regular file", slashRel)
		}
		if st.Size() > maxReadBytes {
			return "", "", fmt.Errorf("GRAB-E401: %q exceeds 10 MiB read limit", slashRel)
		}
	}

	return realTarget, slashRel, nil
}

func readBoundedFile(absPath string) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("GRAB-E401: target is not a regular file")
	}
	if st.Size() > maxReadBytes {
		return nil, fmt.Errorf("GRAB-E401: file exceeds 10 MiB read limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReadBytes {
		return nil, fmt.Errorf("GRAB-E401: file exceeds 10 MiB read limit")
	}
	return data, nil
}
