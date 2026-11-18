package phpmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"herdlite/internal/paths"
)

// LockChanges serializes PHP CLI mutations across compilation and confirmation.
// The kernel releases it even if the updater is killed.
func LockChanges(p paths.Paths) (func(), error) {
	if err := os.MkdirAll(p.RuntimeDir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(p.RuntimeDir, "php-changes.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another PHP command is in progress: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
