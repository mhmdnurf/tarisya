package buildinfo

import "fmt"

// These values are replaced with release metadata through linker flags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func IsVersionCommand(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-version":
		return true
	default:
		return false
	}
}

func String(binary string) string {
	return fmt.Sprintf("%s %s\ncommit: %s\nbuilt: %s", binary, Version, Commit, BuildDate)
}
