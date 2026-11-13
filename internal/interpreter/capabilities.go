package interpreter

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxFileSizeBytes = 10 * 1024 * 1024 // 10 MiB

var (
	errCapabilityDenied        = errors.New("permission denied")
	errNetworkPermissionDenied = errors.New("network permission denied")
	errInvalidNetworkURL       = errors.New("invalid or unsupported HTTP URL")
)

// Permissions defines explicit filesystem and network capabilities granted to a
// GRAB execution. The zero value grants no capabilities (default-deny).
type Permissions struct {
	AllowRead  []string
	AllowWrite []string
	AllowNet   []string
}

// ParseAllowNetList parses and validates a comma-separated --allow-net flag value,
// rejecting empty entries, wildcards ('*'), schemes, or paths.
func ParseAllowNetList(raw string) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" || strings.ContainsAny(entry, "* \t/\\?#@") || strings.Contains(entry, "://") {
			return nil, false
		}
		host, port, hasPort, ok := splitAllowNetEntry(entry)
		if !ok || host == "" {
			return nil, false
		}
		if hasPort {
			out = append(out, net.JoinHostPort(strings.ToLower(host), port))
		} else {
			out = append(out, strings.ToLower(host))
		}
	}
	return out, len(out) > 0
}

func splitAllowNetEntry(entry string) (host string, port string, hasPort bool, ok bool) {
	if strings.HasPrefix(entry, "[") {
		if strings.HasSuffix(entry, "]") {
			inner := strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]")
			if net.ParseIP(inner) == nil {
				return "", "", false, false
			}
			return inner, "", false, true
		}
		h, p, err := net.SplitHostPort(entry)
		if err != nil || net.ParseIP(h) == nil {
			return "", "", false, false
		}
		portNum, err := strconv.Atoi(p)
		if err != nil || portNum < 1 || portNum > 65535 {
			return "", "", false, false
		}
		return h, strconv.Itoa(portNum), true, true
	}
	if strings.Count(entry, ":") == 1 {
		h, p, err := net.SplitHostPort(entry)
		if err != nil || h == "" {
			return "", "", false, false
		}
		portNum, err := strconv.Atoi(p)
		if err != nil || portNum < 1 || portNum > 65535 {
			return "", "", false, false
		}
		return h, strconv.Itoa(portNum), true, true
	}
	if strings.Contains(entry, ":") {
		// Unbracketed IPv6 literal without port
		if net.ParseIP(entry) == nil {
			return "", "", false, false
		}
		return entry, "", false, true
	}
	return entry, "", false, true
}

// checkNetworkURL validates rawURL's scheme and host against p.AllowNet and SSRF
// policies, returning the parsed URL and whether local/private IPs were
// explicitly allowlisted for this target.
func (p Permissions) checkNetworkURL(rawURL string) (*url.URL, bool, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') {
		return nil, false, errInvalidNetworkURL
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, false, errInvalidNetworkURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false, errInvalidNetworkURL
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return nil, false, errInvalidNetworkURL
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return nil, false, errInvalidNetworkURL
		}
		port = strconv.Itoa(portNum)
	}

	// Cloud metadata IP 169.254.169.254 (and link-local 169.254.0.0/16) is always blocked unconditionally.
	if ip := net.ParseIP(hostname); ip != nil && isAlwaysBlockedIP(ip) {
		return nil, false, errNetworkPermissionDenied
	}

	if len(p.AllowNet) == 0 {
		return nil, false, errNetworkPermissionDenied
	}

	matched := false
	allowLocalOrPrivate := false
	for _, rawEntry := range p.AllowNet {
		entryHost, entryPort, hasPort, ok := splitAllowNetEntry(strings.TrimSpace(rawEntry))
		if !ok {
			continue
		}
		entryHost = strings.ToLower(entryHost)
		if entryHost != hostname {
			continue
		}
		if hasPort {
			if entryPort != port {
				continue
			}
		} else {
			defaultPort := "80"
			if scheme == "https" {
				defaultPort = "443"
			}
			if port != defaultPort {
				continue
			}
		}
		matched = true
		if entryHost == "localhost" {
			allowLocalOrPrivate = true
		} else if ip := net.ParseIP(entryHost); ip != nil && !isAlwaysBlockedIP(ip) && isLoopbackOrPrivateIP(ip) {
			allowLocalOrPrivate = true
		}
		break
	}
	if !matched {
		return nil, false, errNetworkPermissionDenied
	}

	// If hostname is already an IP literal, verify SSRF policy immediately.
	if ip := net.ParseIP(hostname); ip != nil {
		if isBlockedSSRFIP(ip, allowLocalOrPrivate) {
			return nil, false, errNetworkPermissionDenied
		}
	} else if hostname == "localhost" && !allowLocalOrPrivate {
		return nil, false, errNetworkPermissionDenied
	}

	return u, allowLocalOrPrivate, nil
}

func isAlwaysBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 0.0.0.0/8, 169.254.0.0/16 (cloud metadata / link-local), 255.255.255.255
		if ip4[0] == 0 || (ip4[0] == 169 && ip4[1] == 254) || (ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255) {
			return true
		}
	}
	return false
}

func isLoopbackOrPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		return false
	}
	// IPv6 Unique Local Address (fc00::/7)
	return len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc
}

func isBlockedSSRFIP(ip net.IP, allowLocalOrPrivate bool) bool {
	if isAlwaysBlockedIP(ip) {
		return true
	}
	if isLoopbackOrPrivateIP(ip) {
		return !allowLocalOrPrivate
	}
	return false
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

// IsSensitiveSegment reports whether a single path segment matches GRAB's
// unconditional sensitive-file blocklist (.git, .env, .ssh, *.pem, *.key, etc.).
func IsSensitiveSegment(seg string) bool { return isSensitiveSegment(seg) }

// HasBlockedSegments reports whether rawPath contains NUL bytes, parent
// traversal (".."), or sensitive segments.
func HasBlockedSegments(rawPath string) bool { return hasBlockedSegments(rawPath) }

// IsPathWithinRoot reports whether target stays strictly within root without
// traversing sensitive segments.
func IsPathWithinRoot(target, root string) bool { return isPathWithinRoot(target, root) }

// EvalExistingSymlinks resolves symlinks for existing path prefixes so even
// not-yet-created target files are confined within their real parent directory.
func EvalExistingSymlinks(absPath string) (string, error) { return evalExistingSymlinks(absPath) }

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
