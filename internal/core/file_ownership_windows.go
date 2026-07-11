//go:build windows

package core

import "os"

func preserveFileOwnership(_ string, _ os.FileInfo) error { return nil }
