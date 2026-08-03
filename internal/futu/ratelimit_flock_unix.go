//go:build !windows

package futu

import (
	"os"
	"syscall"
)

// flockExclusive takes a blocking exclusive advisory lock on f (flock(2));
// present on every non-Windows release target (linux/darwin).
func flockExclusive(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

// flockRelease releases the lock taken by flockExclusive.
func flockRelease(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
