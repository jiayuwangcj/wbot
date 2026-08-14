//go:build windows

package backtestpush

import (
	"os"
	"time"
)

const staleWindowsLock = 2 * time.Minute

type fileLock struct {
	file *os.File
	path string
}

func acquireFileLock(path string) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if os.IsExist(err) {
		// Windows has no flock in the standard library. An exclusive creation
		// prevents concurrent CLI processes from both sending; a lock older
		// than two minutes is recoverable after an abnormal process exit (the
		// Discord client timeout is 15 seconds).
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > staleWindowsLock {
			if removeErr := os.Remove(path); removeErr == nil {
				file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return &fileLock{file: file, path: path}, nil
}

func (l *fileLock) release() {
	if l != nil && l.file != nil {
		_ = l.file.Close()
		_ = os.Remove(l.path)
	}
}
