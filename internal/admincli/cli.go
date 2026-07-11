package admincli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mhmdnurf/tarisya/internal/buildinfo"
	"github.com/mhmdnurf/tarisya/internal/core"
)

const usage = `Usage:
  tarisya backup [--database URL] [--output PATH]
  tarisya restore [--database URL] [--checksum PATH] BACKUP
  tarisya database check [--database URL]
  tarisya doctor [--config PATH] [--database URL] [--health-url URL]
  tarisya version
  tarisya help

Environment:
  TARISYA_DATABASE_URL   SQLite database URL used when --database is omitted
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, buildinfo.String("tarisya"))
		return 0
	case "backup":
		return runBackup(ctx, args[1:], stdout, stderr)
	case "restore":
		return runRestore(ctx, args[1:], stdout, stderr)
	case "database":
		return runDatabase(ctx, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runRestore(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database", "", "target SQLite database URL")
	checksumPath := flags.String("checksum", "", "backup SHA-256 sidecar path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: tarisya restore [--database URL] [--checksum PATH] BACKUP")
		return 2
	}
	if *databaseURL == "" {
		*databaseURL = strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL"))
	}
	if *databaseURL == "" {
		fmt.Fprintln(stderr, "TARISYA_DATABASE_URL or --database is required")
		return 1
	}
	result, err := core.RestoreDatabase(ctx, *databaseURL, flags.Arg(0), *checksumPath)
	if err != nil {
		fmt.Fprintf(stderr, "restore failed: %v\n", err)
		return 1
	}
	if !result.ChecksumVerified {
		fmt.Fprintln(stderr, "warning: no checksum sidecar was available; database integrity was verified")
	}
	if _, err := fmt.Fprintf(stdout, "restored: %s\npre-restore backup: %s\npre-restore checksum: %s\n", result.DatabasePath, result.PreRestoreBackup, result.PreRestoreChecksum); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}

func runBackup(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database", "", "SQLite database URL")
	outputPath := flags.String("output", "", "backup output path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "backup does not accept positional arguments")
		return 2
	}
	if *databaseURL == "" {
		*databaseURL = strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL"))
	}
	if *databaseURL == "" {
		fmt.Fprintln(stderr, "TARISYA_DATABASE_URL or --database is required")
		return 1
	}
	result, err := core.BackupDatabase(ctx, *databaseURL, *outputPath)
	if err != nil {
		fmt.Fprintf(stderr, "backup failed: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "backup: %s\nchecksum: %s\nsha256: %s\nsize: %d bytes\n", result.Path, result.ChecksumPath, result.SHA256, result.Size); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}

func runDatabase(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintf(stderr, "usage: tarisya database check [--database URL]\n")
		return 2
	}
	flags := flag.NewFlagSet("database check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database", "", "SQLite database URL")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "database check does not accept positional arguments")
		return 2
	}
	if *databaseURL == "" {
		*databaseURL = strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL"))
	}
	if *databaseURL == "" {
		fmt.Fprintln(stderr, "TARISYA_DATABASE_URL or --database is required")
		return 1
	}
	if err := core.CheckDatabase(ctx, *databaseURL); err != nil {
		fmt.Fprintf(stderr, "database check failed: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "database check passed"); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}
