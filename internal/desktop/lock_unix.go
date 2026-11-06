//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"syscall"
)

// lock keeps two opens of the same profile from rebuilding the copy at once.
func lock(in Instance) (func(), error) {
	if err := os.MkdirAll(in.dir(), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(in.dir(), "."+in.Label()+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
