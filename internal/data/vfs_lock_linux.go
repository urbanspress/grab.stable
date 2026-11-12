//go:build linux && amd64

package data

import (
	"golang.org/x/sys/unix"
	"os"
)

func syncDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func singleLink(f *os.File) bool {
	var stat unix.Stat_t
	return unix.Fstat(int(f.Fd()), &stat) == nil && stat.Nlink == 1
}
func fileRange(f *os.File, start, length uint32, exclusive, unlock bool) error {
	lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: int64(start), Len: int64(length)}
	if exclusive {
		lock.Type = unix.F_WRLCK
	}
	if unlock {
		lock.Type = unix.F_UNLCK
	}
	return unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, &lock)
}
