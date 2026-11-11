package interpreter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const maxFileSizeBytes = 10 * 1024 * 1024 // 10 MiB

var errCapabilityDenied = errors.New("permission denied")

// Permissions defines explicit filesystem capabilities granted to a GRAB
// execution. The zero value grants no filesystem capabilities (default-deny).
type Permissions struct {
	AllowRead  []string
	AllowWrite []string
}

func isSensitiveSegment(seg string) bool {
	lower := strings.ToLower(strings.TrimSpace(seg))
	if lower == "" {
		return false
	}
	switch lower {
	case ".git", ".env", ".ssh", "id_rsa", "id_ed25519":
		return true
	}
	if strings.HasPrefix(lower, ".env.") {
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	return false
}

func hasBlockedSegments(rawPath string) bool {
	if rawPath == "" || strings.ContainsRune(rawPath, '\x00') {
		return true
	}
	segments := strings.FieldsFunc(rawPath, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(segments) == 0 {
		return true
	}
	for _, seg := range segments {
		if seg == ".." || isSensitiveSegment(seg) {
			return true
		}
	}
	return false
}

func isPathWithinRoot(target, root string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel != "." {
		for _, seg := range strings.Split(rel, string(filepath.Separator)) {
			if seg == ".." || isSensitiveSegment(seg) {
				return false
			}
		}
	}
	return true
}

func evalExistingSymlinks(absPath string) (string, error) {
	real, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absPath)
	if parent == absPath {
		return absPath, nil
	}
	realParent, err := evalExistingSymlinks(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(realParent, filepath.Base(absPath)), nil
}

func (p Permissions) resolveReadPath(rawPath string) (string, error) {
	return resolveCapabilityPath(rawPath, p.AllowRead)
}

func (p Permissions) resolveWritePath(rawPath string) (string, error) {
	return resolveCapabilityPath(rawPath, p.AllowWrite)
}

func resolveCapabilityPath(rawPath string, allowedRoots []string) (string, error) {
	if hasBlockedSegments(rawPath) || len(allowedRoots) == 0 {
		return "", errCapabilityDenied
	}

	for _, allowed := range allowedRoots {
		if strings.TrimSpace(allowed) == "" || strings.ContainsRune(allowed, '\x00') {
			continue
		}
		absRoot, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		realRoot, err := evalExistingSymlinks(absRoot)
		if err != nil {
			continue
		}

		var candidates []string
		if filepath.IsAbs(rawPath) {
			candidates = []string{filepath.Clean(rawPath)}
		} else {
			if absFromWD, err := filepath.Abs(rawPath); err == nil && isPathWithinRoot(absFromWD, absRoot) {
				candidates = append(candidates, absFromWD)
			}
			candidates = append(candidates, filepath.Join(realRoot, rawPath))
		}

		for _, candidate := range candidates {
			realCandidate, err := evalExistingSymlinks(candidate)
			if err != nil {
				continue
			}
			if isPathWithinRoot(realCandidate, realRoot) {
				return realCandidate, nil
			}
		}
	}

	return "", errCapabilityDenied
}
