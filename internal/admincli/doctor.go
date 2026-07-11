package admincli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/core"
)

const defaultDoctorConfigPath = "/etc/tarisya/core.env"

type doctorCheck struct {
	name   string
	err    error
	failed bool
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultDoctorConfigPath, "Core environment file")
	databaseURL := flags.String("database", "", "SQLite database URL")
	healthURL := flags.String("health-url", "", "Core health endpoint")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "doctor does not accept positional arguments")
		return 2
	}

	checks := make([]doctorCheck, 0, 8)
	values, configErr := godotenv.Read(*configPath)
	if configErr == nil {
		configErr = validateDoctorConfig(values)
	}
	checks = append(checks, newDoctorCheck("Configuration loaded", configErr))

	if *databaseURL == "" {
		*databaseURL = strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL"))
	}
	if *databaseURL == "" {
		*databaseURL = strings.TrimSpace(values["TARISYA_DATABASE_URL"])
	}

	var diagnostics core.DatabaseDiagnostics
	var databaseErr error
	if *databaseURL == "" {
		databaseErr = errors.New("database URL is unavailable")
		checks = append(checks, databaseUnavailableChecks(databaseErr)...)
	} else {
		diagnostics, databaseErr = core.DiagnoseDatabase(ctx, *databaseURL)
		checks = append(checks, newDoctorCheck("SQLite reachable", databaseErr))
		if databaseErr != nil {
			checks = append(checks, databaseUnavailableChecks(errors.New("not checked because SQLite is unreachable"))[1:]...)
		} else {
			checks = append(checks,
				newDoctorCheck("WAL enabled", settingError(diagnostics.JournalModeErr, diagnostics.JournalMode == "wal", fmt.Sprintf("journal_mode is %q, expected \"wal\"", diagnostics.JournalMode))),
				newDoctorCheck("Foreign keys enabled", settingError(diagnostics.ForeignKeysErr, diagnostics.ForeignKeys, "foreign_keys is disabled")),
				newDoctorCheck("Storage writable", settingError(diagnostics.StorageErr, diagnostics.StorageWritable, "database is not writable")),
				newDoctorCheck("Migration up-to-date", migrationError(diagnostics)),
			)
		}
	}

	if *healthURL == "" {
		*healthURL = strings.TrimSpace(os.Getenv("TARISYA_HEALTH_URL"))
	}
	if *healthURL == "" {
		*healthURL = localHealthURL(values["TARISYA_CORE_ADDRESS"])
	}
	checks = append(checks, newDoctorCheck("HTTP server healthy", checkHealth(ctx, *healthURL)))

	if *databaseURL == "" {
		checks = append(checks, newDoctorCheck("API keys configured", errors.New("not checked because the database URL is unavailable")))
	} else if databaseErr != nil {
		checks = append(checks, newDoctorCheck("API keys configured", errors.New("not checked because SQLite is unreachable")))
	} else {
		checks = append(checks, newDoctorCheck("API keys configured", apiKeysError(diagnostics)))
	}

	failures := 0
	for _, check := range checks {
		if check.failed {
			failures++
			fmt.Fprintf(stdout, "✗ %s: %v\n", check.name, check.err)
		} else {
			fmt.Fprintf(stdout, "✓ %s\n", check.name)
		}
	}
	if failures == 0 {
		fmt.Fprintln(stdout, "\nTarisya is healthy.")
		return 0
	}
	fmt.Fprintf(stdout, "\nDoctor found %d problem(s).\n", failures)
	if errors.Is(configErr, os.ErrPermission) {
		fmt.Fprintln(stdout, "\nSome checks require access to protected Tarisya files.")
		fmt.Fprintln(stdout, "Run again as an administrator:\n\n  sudo tarisya doctor")
	}
	return 1
}

func newDoctorCheck(name string, err error) doctorCheck {
	return doctorCheck{name: name, err: err, failed: err != nil}
}

func validateDoctorConfig(values map[string]string) error {
	if strings.TrimSpace(values["TARISYA_DATABASE_URL"]) == "" {
		return errors.New("TARISYA_DATABASE_URL is missing")
	}
	if len(strings.TrimSpace(values["TARISYA_JWT_SECRET"])) < 32 {
		return errors.New("TARISYA_JWT_SECRET must contain at least 32 characters")
	}
	return nil
}

func databaseUnavailableChecks(err error) []doctorCheck {
	return []doctorCheck{
		newDoctorCheck("SQLite reachable", err),
		newDoctorCheck("WAL enabled", err),
		newDoctorCheck("Foreign keys enabled", err),
		newDoctorCheck("Storage writable", err),
		newDoctorCheck("Migration up-to-date", err),
	}
}

func settingError(queryErr error, enabled bool, message string) error {
	if queryErr != nil {
		return queryErr
	}
	if !enabled {
		return errors.New(message)
	}
	return nil
}

func migrationError(result core.DatabaseDiagnostics) error {
	if result.MigrationErr != nil {
		return result.MigrationErr
	}
	if !result.MigrationCurrent {
		return fmt.Errorf("schema version is %d, expected 1", result.SchemaVersion)
	}
	return nil
}

func apiKeysError(result core.DatabaseDiagnostics) error {
	if result.APIKeysErr != nil {
		return result.APIKeysErr
	}
	if result.ActiveAPIKeys == 0 {
		return errors.New("no active API keys found")
	}
	return nil
}

func localHealthURL(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return "http://127.0.0.1:8081/health"
	}
	if strings.HasPrefix(address, ":") {
		return "http://127.0.0.1" + address + "/health"
	}
	if strings.HasPrefix(address, "0.0.0.0:") {
		return "http://127.0.0.1:" + strings.TrimPrefix(address, "0.0.0.0:") + "/health"
	}
	return "http://" + address + "/health"
}

func checkHealth(ctx context.Context, healthURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("server returned %s", response.Status)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode health response: %w", err)
	}
	if body.Status != "ok" {
		return fmt.Errorf("health status is %q", body.Status)
	}
	return nil
}
