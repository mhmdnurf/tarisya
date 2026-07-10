package migrations

import (
	"embed"
	"strings"
)

// Files contains all versioned database migrations.
//
//go:embed *.sql
var Files embed.FS

//go:embed 000001_initial_schema.up.sql
var initialSchema string

//go:embed 000001_initial_schema.down.sql
var initialSchemaDown string

func InitialSchema() string { return initialSchema }

func InitialSchemaDown() string { return initialSchemaDown }

func Statements(sql string) []string {
	return strings.Split(sql, ";")
}
