package single

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock is an exclusive flock on hub_root/syncd.lock so two syncd
// processes cannot share a worktree or journal.
type Lock struct {
	f *os.File
}

func Acquire(hubRoot string) (*Lock, error) {
	if err := os.MkdirAll(hubRoot, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(hubRoot, "syncd.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another syncd already holds %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
