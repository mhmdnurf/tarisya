//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package core

import "os"

func replaceFileAtomically(source, target string) error {
	return os.Rename(source, target)
}
