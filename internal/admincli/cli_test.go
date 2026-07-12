package admincli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhmdnurf/tarisya/internal/core"
)

func TestRunHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("Run(%q) code = %d, want 0; stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "tarisya database check") {
			t.Fatalf("Run(%q) output = %q, want usage", args, stdout.String())
		}
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tarisya dev") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunDatabaseCheck(t *testing.T) {
	ctx := context.Background()
	databaseURL := "file:" + filepath.Join(t.TempDir(), "tarisya.db")
	store, err := core.OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()

	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"database", "check", "--database", databaseURL}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("database check code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "database check passed" {
		t.Fatalf("database check output = %q", stdout.String())
	}
}

func TestRunDoctorHealthy(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "Application Support", "Tarisya")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	databaseURL := "file:" + filepath.Join(directory, "tarisya.db")
	store, err := core.OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, "Doctor User", "doctor@example.com", "hash", "srv_doctor", "tar_doctor"); err != nil {
		t.Fatal(err)
	}

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer health.Close()
	configPath := filepath.Join(directory, "core.env")
	config := "TARISYA_DATABASE_URL=\"" + databaseURL + "\"\n" +
		"TARISYA_JWT_SECRET=doctor-secret-that-is-at-least-32-characters\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"doctor", "--config", configPath, "--health-url", health.URL}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor code = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, expected := range []string{
		"✓ Configuration loaded",
		"✓ SQLite reachable",
		"✓ WAL enabled",
		"✓ Foreign keys enabled",
		"✓ Storage writable",
		"✓ Migration up-to-date",
		"✓ HTTP server healthy",
		"✓ API keys configured",
		"Tarisya is healthy.",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("doctor output %q does not contain %q", stdout.String(), expected)
		}
	}
}

func TestRunDoctorReportsMissingAPIKeys(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databaseURL := "file:" + filepath.Join(directory, "tarisya.db")
	store, err := core.OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer health.Close()
	configPath := filepath.Join(directory, "core.env")
	config := "TARISYA_DATABASE_URL=" + databaseURL + "\n" +
		"TARISYA_JWT_SECRET=doctor-secret-that-is-at-least-32-characters\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"doctor", "--config", configPath, "--health-url", health.URL}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "✗ API keys configured: no active API keys found") {
		t.Fatalf("doctor output = %q", stdout.String())
	}
}

func TestRunBackup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databaseURL := "file:" + filepath.Join(directory, "tarisya.db")
	store, err := core.OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()
	outputPath := filepath.Join(directory, "backup.db")

	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"backup", "--database", databaseURL, "--output", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("backup code = %d, want 0; stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"backup: " + outputPath, "checksum: " + outputPath + ".sha256", "sha256:", "size:"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("backup output %q does not contain %q", stdout.String(), expected)
		}
	}
}

func TestRunBackupHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"backup", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup help code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "-output") {
		t.Fatalf("backup help output = %q", stderr.String())
	}
}

func TestRunRestore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	targetURL := "file:" + filepath.Join(directory, "target.db")
	target, err := core.OpenStore(ctx, targetURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	target.Close()

	sourceURL := "file:" + filepath.Join(directory, "source.db")
	source, err := core.OpenStore(ctx, sourceURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(directory, "backup.db")
	if _, err := core.BackupDatabase(ctx, sourceURL, backupPath); err != nil {
		t.Fatal(err)
	}
	source.Close()

	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"restore", "--database", targetURL, backupPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("restore code = %d, want 0; stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"restored:", "pre-restore backup:", "pre-restore checksum:"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("restore output %q does not contain %q", stdout.String(), expected)
		}
	}
}

func TestRunRestoreRequiresOneBackup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"restore"}, &stdout, &stderr); code != 2 {
		t.Fatalf("restore code = %d, want 2", code)
	}
}

func TestRunDatabaseCheckRequiresURL(t *testing.T) {
	t.Setenv("TARISYA_DATABASE_URL", "")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"database", "check"}, &stdout, &stderr); code != 1 {
		t.Fatalf("database check code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "TARISYA_DATABASE_URL") {
		t.Fatalf("database check error = %q", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown command code = %d, want 2", code)
	}
}
