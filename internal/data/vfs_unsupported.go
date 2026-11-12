//go:build (!windows && !linux) || !amd64

package data

// Disk databases fail closed until the platform's confined writable VFS has
// passed the same path-race, locking and durability tests as Windows/amd64.
// In-memory and remote databases do not use this VFS.
type confinedVFS struct{}

func newConfinedVFS(string) (*confinedVFS, error) {
	return nil, fail(Denied, "confined SQLite disk access is unavailable on this platform")
}
func (*confinedVFS) Name() string { return "" }
func (*confinedVFS) Close() error { return nil }
