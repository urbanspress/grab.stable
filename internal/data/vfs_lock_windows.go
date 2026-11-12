//go:build windows && amd64

package data

import (
	"golang.org/x/sys/windows"
	"os"
)

// Windows SQLite does not request a separate directory flush. File.Sync uses
// FlushFileBuffers for the database and rollback journal themselves.
func syncDirectory(_ *os.Root) error { return nil }

func singleLink(f *os.File) bool {
	var identity windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &identity) == nil && identity.NumberOfLinks == 1
}
func fileRange(f *os.File, start, length uint32, exclusive, unlock bool) error {
	o := windows.Overlapped{Offset: start}
	if unlock {
		return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, length, 0, &o)
	}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, length, 0, &o)
}
