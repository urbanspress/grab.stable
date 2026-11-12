//go:build (windows || linux) && amd64

package data

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

// confinedVFS is a rollback-journal VFS. Every open is relative to a held
// os.Root, and only the configured database and its journal can be accessed.
// WAL/shared-memory and disk temporary files fail closed in this initial VFS.
// It uses SQLite's documented lock byte ranges for interprocess coordination.
type confinedVFS struct {
	root       *os.Root
	base, name string
	ptr, cname uintptr
	tls        *libc.TLS
	files      int
}

func (v *confinedVFS) Name() string { return v.name }

// These are the public sqlite3_vfs and sqlite3_io_methods C ABI layouts on
// Windows/amd64. Callback fields store ccgo function pointers, following the
// driver's VFS bridge. This file is intentionally platform constrained.
type vfsABI struct {
	version, size, maxPath                                                                                                                                           int32
	next, name, appData                                                                                                                                              uintptr
	open, delete, access, fullPath, dlOpen, dlError, dlSym, dlClose, random, sleep, currentTime, lastError, currentTime64, systemCall, getSystemCall, nextSystemCall uintptr
}
type ioABI struct {
	version                                                                                   int32
	close, read, write, truncate, sync, size, lock, unlock, reserved, control, sector, device uintptr
}
type fileABI struct{ methods, id uintptr }
type confinedFile struct {
	file          *os.File
	vfs           *confinedVFS
	level         int32
	readonly      bool
	syncDirectory bool
}

var vfsState = struct {
	sync.Mutex
	next    uintptr
	vfs     map[uintptr]*confinedVFS
	files   map[uintptr]*confinedFile
	methods uintptr
}{vfs: map[uintptr]*confinedVFS{}, files: map[uintptr]*confinedFile{}}

func initVFSMethods(tls *libc.TLS) uintptr {
	if vfsState.methods != 0 {
		return vfsState.methods
	}
	p := libc.Xcalloc(tls, 1, uint64(unsafe.Sizeof(ioABI{})))
	if p == 0 {
		return 0
	}
	m := (*ioABI)(unsafe.Pointer(p))
	m.version = 1
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(&m.close)) = confinedClose
	*(*func(*libc.TLS, uintptr, uintptr, int32, int64) int32)(unsafe.Pointer(&m.read)) = confinedRead
	*(*func(*libc.TLS, uintptr, uintptr, int32, int64) int32)(unsafe.Pointer(&m.write)) = confinedWrite
	*(*func(*libc.TLS, uintptr, int64) int32)(unsafe.Pointer(&m.truncate)) = confinedTruncate
	*(*func(*libc.TLS, uintptr, int32) int32)(unsafe.Pointer(&m.sync)) = confinedSync
	*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(&m.size)) = confinedSize
	*(*func(*libc.TLS, uintptr, int32) int32)(unsafe.Pointer(&m.lock)) = confinedLock
	*(*func(*libc.TLS, uintptr, int32) int32)(unsafe.Pointer(&m.unlock)) = confinedUnlock
	*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(&m.reserved)) = confinedReserved
	*(*func(*libc.TLS, uintptr, int32, uintptr) int32)(unsafe.Pointer(&m.control)) = confinedControl
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(&m.sector)) = confinedSector
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(&m.device)) = confinedDevice
	vfsState.methods = p
	return p
}

func newConfinedVFS(path string) (*confinedVFS, error) {
	if !filepath.IsAbs(path) {
		return nil, fail(Denied, "database path must be canonical")
	}
	before, e := os.Stat(filepath.Dir(path))
	if e != nil || !before.IsDir() {
		return nil, fail(Denied, "database directory is unavailable")
	}
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, fail(Denied, "database directory is unavailable")
	}
	after, e := root.Stat(".")
	if e != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fail(Denied, "database directory changed while opening")
	}
	v := &confinedVFS{root: root, base: filepath.Base(path), tls: libc.NewTLS()}
	vfsState.Lock()
	defer vfsState.Unlock()
	vfsState.next++
	id := vfsState.next
	v.name = "grab_confined_" + strconvUint(id)
	v.cname, e = libc.CString(v.name)
	if e != nil {
		_ = root.Close()
		v.tls.Close()
		return nil, fail(Operation, "database VFS allocation failed")
	}
	v.ptr = libc.Xcalloc(v.tls, 1, uint64(unsafe.Sizeof(vfsABI{})))
	if v.ptr == 0 || initVFSMethods(v.tls) == 0 {
		v.dispose()
		return nil, fail(Operation, "database VFS allocation failed")
	}
	base := sqlite3.Xsqlite3_vfs_find(v.tls, 0)
	if base == 0 {
		v.dispose()
		return nil, fail(Operation, "database VFS unavailable")
	}
	api := (*vfsABI)(unsafe.Pointer(v.ptr))
	*api = *(*vfsABI)(unsafe.Pointer(base))
	api.version = 1
	api.size = int32(unsafe.Sizeof(fileABI{}))
	api.maxPath = 1024
	api.name = v.cname
	api.appData = id
	api.next = 0
	*(*func(*libc.TLS, uintptr, uintptr, uintptr, int32, uintptr) int32)(unsafe.Pointer(&api.open)) = confinedOpen
	*(*func(*libc.TLS, uintptr, uintptr, int32) int32)(unsafe.Pointer(&api.delete)) = confinedDelete
	*(*func(*libc.TLS, uintptr, uintptr, int32, uintptr) int32)(unsafe.Pointer(&api.access)) = confinedAccess
	*(*func(*libc.TLS, uintptr, uintptr, int32, uintptr) int32)(unsafe.Pointer(&api.fullPath)) = confinedFullPath
	api.dlOpen = 0
	api.dlError = 0
	api.dlSym = 0
	api.dlClose = 0
	vfsState.vfs[id] = v
	if sqlite3.Xsqlite3_vfs_register(v.tls, v.ptr, 0) != sqlite3.SQLITE_OK {
		delete(vfsState.vfs, id)
		v.dispose()
		return nil, fail(Operation, "database VFS registration failed")
	}
	return v, nil
}

func strconvUint(n uintptr) string {
	const digits = "0123456789abcdef"
	var b [32]byte
	i := len(b)
	for {
		i--
		b[i] = digits[n%16]
		n /= 16
		if n == 0 {
			return string(b[i:])
		}
	}
}
func (v *confinedVFS) dispose() {
	if v.ptr != 0 {
		libc.Xfree(v.tls, v.ptr)
	}
	if v.cname != 0 {
		libc.Xfree(v.tls, v.cname)
	}
	_ = v.root.Close()
	v.tls.Close()
}
func (v *confinedVFS) Close() error {
	vfsState.Lock()
	defer vfsState.Unlock()
	if v.ptr == 0 {
		return nil
	}
	if v.files != 0 {
		return fail(Ownership, "database VFS still in use")
	}
	api := (*vfsABI)(unsafe.Pointer(v.ptr))
	delete(vfsState.vfs, api.appData)
	sqlite3.Xsqlite3_vfs_unregister(v.tls, v.ptr)
	v.dispose()
	v.ptr = 0
	return nil
}
func getVFS(p uintptr) *confinedVFS {
	vfsState.Lock()
	defer vfsState.Unlock()
	return vfsState.vfs[(*vfsABI)(unsafe.Pointer(p)).appData]
}
func getFile(p uintptr) *confinedFile {
	vfsState.Lock()
	defer vfsState.Unlock()
	return vfsState.files[(*fileABI)(unsafe.Pointer(p)).id]
}
func (v *confinedVFS) nameFor(raw string) (string, bool) {
	raw = strings.TrimPrefix(raw, "/grab/")
	if raw == v.base || raw == v.base+"-journal" {
		return raw, true
	}
	return "", false
}

func confinedFullPath(_ *libc.TLS, p, z uintptr, n int32, out uintptr) int32 {
	v := getVFS(p)
	name, ok := v.nameFor(libc.GoString(z))
	if !ok {
		return sqlite3.SQLITE_CANTOPEN
	}
	full := "/grab/" + name
	if len(full)+1 > int(n) {
		return sqlite3.SQLITE_CANTOPEN
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(out)), int(n))
	copy(buf, full)
	buf[len(full)] = 0
	return sqlite3.SQLITE_OK
}
func confinedOpen(_ *libc.TLS, p, z, file uintptr, flags int32, out uintptr) int32 {
	if z == 0 {
		return sqlite3.SQLITE_CANTOPEN
	}
	v := getVFS(p)
	name, ok := v.nameFor(libc.GoString(z))
	if !ok {
		return sqlite3.SQLITE_CANTOPEN
	}
	if flags&(sqlite3.SQLITE_OPEN_WAL|sqlite3.SQLITE_OPEN_DELETEONCLOSE) != 0 {
		return sqlite3.SQLITE_CANTOPEN
	}
	readonly := flags&sqlite3.SQLITE_OPEN_READONLY != 0
	mode := os.O_RDWR
	if readonly {
		mode = os.O_RDONLY
	}
	// Inspect without following symlinks, then compare the opened identity.
	// Root prevents path traversal even during a rename/symlink race. O_EXCL
	// makes creation atomic; no truncation occurs before identity validation.
	before, e := v.root.Lstat(name)
	exists := e == nil
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return sqlite3.SQLITE_CANTOPEN
	}
	if exists && (!before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0) {
		return sqlite3.SQLITE_CANTOPEN
	}
	if !exists {
		if readonly || flags&sqlite3.SQLITE_OPEN_CREATE == 0 {
			return sqlite3.SQLITE_CANTOPEN
		}
		mode |= os.O_CREATE | os.O_EXCL
	}
	f, e := v.root.OpenFile(name, mode, 0600)
	if e != nil {
		return sqlite3.SQLITE_CANTOPEN
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || exists && !os.SameFile(before, st) {
		_ = f.Close()
		return sqlite3.SQLITE_CANTOPEN
	}
	if !singleLink(f) {
		_ = f.Close()
		return sqlite3.SQLITE_CANTOPEN
	}
	vfsState.Lock()
	vfsState.next++
	id := vfsState.next
	vfsState.files[id] = &confinedFile{file: f, vfs: v, readonly: readonly, syncDirectory: !exists}
	v.files++
	methods := vfsState.methods
	vfsState.Unlock()
	*(*fileABI)(unsafe.Pointer(file)) = fileABI{methods: methods, id: id}
	if out != 0 {
		*(*int32)(unsafe.Pointer(out)) = flags
	}
	return sqlite3.SQLITE_OK
}
func confinedDelete(_ *libc.TLS, p, z uintptr, dirSync int32) int32 {
	v := getVFS(p)
	name, ok := v.nameFor(libc.GoString(z))
	if !ok || name == v.base {
		return sqlite3.SQLITE_IOERR_DELETE
	}
	if e := v.root.Remove(name); e != nil && !errors.Is(e, os.ErrNotExist) {
		return sqlite3.SQLITE_IOERR_DELETE
	}
	if dirSync != 0 && syncDirectory(v.root) != nil {
		return sqlite3.SQLITE_IOERR_DIR_FSYNC
	}
	return sqlite3.SQLITE_OK
}
func confinedAccess(_ *libc.TLS, p, z uintptr, _ int32, out uintptr) int32 {
	v := getVFS(p)
	name, ok := v.nameFor(libc.GoString(z))
	*(*int32)(unsafe.Pointer(out)) = 0
	if !ok {
		return sqlite3.SQLITE_OK
	}
	st, e := v.root.Lstat(name)
	if e == nil && st.Mode().IsRegular() {
		*(*int32)(unsafe.Pointer(out)) = 1
	}
	return sqlite3.SQLITE_OK
}
func confinedClose(_ *libc.TLS, p uintptr) int32 {
	f := getFile(p)
	_ = confinedUnlock(nil, p, 0)
	e := f.file.Close()
	vfsState.Lock()
	delete(vfsState.files, (*fileABI)(unsafe.Pointer(p)).id)
	f.vfs.files--
	vfsState.Unlock()
	if e != nil {
		return sqlite3.SQLITE_IOERR_CLOSE
	}
	return sqlite3.SQLITE_OK
}
func confinedRead(_ *libc.TLS, p, buf uintptr, n int32, off int64) int32 {
	if n < 0 || off < 0 {
		return sqlite3.SQLITE_IOERR_READ
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n))
	read, e := getFile(p).file.ReadAt(b, off)
	if e == io.EOF {
		clear(b[read:])
		return sqlite3.SQLITE_IOERR_SHORT_READ
	}
	if e != nil {
		return sqlite3.SQLITE_IOERR_READ
	}
	return sqlite3.SQLITE_OK
}
func confinedWrite(_ *libc.TLS, p, buf uintptr, n int32, off int64) int32 {
	f := getFile(p)
	if f.readonly {
		return sqlite3.SQLITE_READONLY
	}
	if n < 0 || off < 0 {
		return sqlite3.SQLITE_IOERR_WRITE
	}
	if _, e := f.file.WriteAt(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n)), off); e != nil {
		return sqlite3.SQLITE_IOERR_WRITE
	}
	return sqlite3.SQLITE_OK
}
func confinedTruncate(_ *libc.TLS, p uintptr, size int64) int32 {
	f := getFile(p)
	if f.readonly {
		return sqlite3.SQLITE_READONLY
	}
	if size < 0 || f.file.Truncate(size) != nil {
		return sqlite3.SQLITE_IOERR_TRUNCATE
	}
	return sqlite3.SQLITE_OK
}
func confinedSync(_ *libc.TLS, p uintptr, _ int32) int32 {
	f := getFile(p)
	if f.file.Sync() != nil {
		return sqlite3.SQLITE_IOERR_FSYNC
	}
	if f.syncDirectory {
		if syncDirectory(f.vfs.root) != nil {
			return sqlite3.SQLITE_IOERR_DIR_FSYNC
		}
		f.syncDirectory = false
	}
	return sqlite3.SQLITE_OK
}
func confinedSize(_ *libc.TLS, p, out uintptr) int32 {
	st, e := getFile(p).file.Stat()
	if e != nil {
		return sqlite3.SQLITE_IOERR_FSTAT
	}
	*(*int64)(unsafe.Pointer(out)) = st.Size()
	return sqlite3.SQLITE_OK
}
func confinedControl(_ *libc.TLS, _ uintptr, _ int32, _ uintptr) int32 {
	return sqlite3.SQLITE_NOTFOUND
}
func confinedSector(_ *libc.TLS, _ uintptr) int32 { return 4096 }
func confinedDevice(_ *libc.TLS, _ uintptr) int32 { return 0 }

const pendingByte = 0x40000000

func confinedLock(_ *libc.TLS, p uintptr, level int32) int32 {
	f := getFile(p)
	if f.level >= level {
		return sqlite3.SQLITE_OK
	}
	if f.level == 0 {
		if fileRange(f.file, pendingByte, 1, false, false) != nil {
			return sqlite3.SQLITE_BUSY
		}
		e := fileRange(f.file, pendingByte+2, 510, false, false)
		_ = fileRange(f.file, pendingByte, 1, false, true)
		if e != nil {
			return sqlite3.SQLITE_BUSY
		}
		f.level = 1
	}
	if level == 2 {
		if fileRange(f.file, pendingByte+1, 1, true, false) != nil {
			return sqlite3.SQLITE_BUSY
		}
		f.level = 2
		return sqlite3.SQLITE_OK
	}
	if level >= 3 && f.level < 3 {
		if fileRange(f.file, pendingByte, 1, true, false) != nil {
			return sqlite3.SQLITE_BUSY
		}
		f.level = 3
	}
	if level == 4 {
		_ = fileRange(f.file, pendingByte+2, 510, false, true)
		if fileRange(f.file, pendingByte+2, 510, true, false) != nil {
			_ = fileRange(f.file, pendingByte+2, 510, false, false)
			return sqlite3.SQLITE_BUSY
		}
		f.level = 4
	}
	return sqlite3.SQLITE_OK
}
func confinedUnlock(_ *libc.TLS, p uintptr, level int32) int32 {
	f := getFile(p)
	if f.level == 0 {
		return sqlite3.SQLITE_OK
	}
	if f.level == 4 {
		_ = fileRange(f.file, pendingByte+2, 510, true, true)
		if level == 1 {
			if fileRange(f.file, pendingByte+2, 510, false, false) != nil {
				return sqlite3.SQLITE_IOERR_UNLOCK
			}
		}
	} else if level == 0 {
		_ = fileRange(f.file, pendingByte+2, 510, false, true)
	}
	if f.level >= 3 {
		_ = fileRange(f.file, pendingByte, 1, true, true)
	}
	if f.level >= 2 {
		_ = fileRange(f.file, pendingByte+1, 1, true, true)
	}
	f.level = level
	return sqlite3.SQLITE_OK
}
func confinedReserved(_ *libc.TLS, p, out uintptr) int32 {
	f := getFile(p)
	*(*int32)(unsafe.Pointer(out)) = 0
	if f.level >= 2 {
		*(*int32)(unsafe.Pointer(out)) = 1
		return sqlite3.SQLITE_OK
	}
	if fileRange(f.file, pendingByte+1, 1, true, false) != nil {
		*(*int32)(unsafe.Pointer(out)) = 1
	} else {
		_ = fileRange(f.file, pendingByte+1, 1, true, true)
	}
	return sqlite3.SQLITE_OK
}
