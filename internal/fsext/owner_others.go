//go:build !windows

package fsext

import (
	"os"
	"syscall"
)

// Owner retrieves the user ID of the owner of the file or directory at the
// specified path.
func Owner(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return ownerFromInfo(info), nil
}

// ownerFromInfo derives the owner user ID from an already-obtained
// os.FileInfo, avoiding a redundant stat.
func ownerFromInfo(info os.FileInfo) int {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid)
	}
	return os.Getuid()
}
