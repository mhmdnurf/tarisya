package buildinfo

import (
	"strings"
	"testing"
)

func TestIsVersionCommand(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-version"}} {
		if !IsVersionCommand(args) {
			t.Fatalf("IsVersionCommand(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{nil, {}, {"serve"}, {"version", "extra"}} {
		if IsVersionCommand(args) {
			t.Fatalf("IsVersionCommand(%q) = true, want false", args)
		}
	}
}

func TestString(t *testing.T) {
	got := String("tarisya-core")
	for _, value := range []string{"tarisya-core", Version, Commit, BuildDate} {
		if !strings.Contains(got, value) {
			t.Fatalf("String() = %q, want it to contain %q", got, value)
		}
	}
}
