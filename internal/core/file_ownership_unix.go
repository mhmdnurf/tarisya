//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package core

import (
	"errors"
	"os"
	"syscall"
)

func preserveFileOwnership(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("read target file ownership")
	}
	return os.Chown(path, int(stat.Uid), int(stat.Gid))
}
