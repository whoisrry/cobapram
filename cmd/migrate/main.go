// Command migrate menjalankan migrasi goose yang di-embed di binary.
//
//	migrate up | down | status | version
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/whoisrry/cobapram/internal/config"
	"github.com/whoisrry/cobapram/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("pemakaian: migrate up|down|status|version")
	}
	cmd := os.Args[1]
	switch cmd {
	case "up", "down", "status", "version":
	default:
		return fmt.Errorf("perintah tidak dikenal: %s", cmd)
	}

	cfg, err := config.LoadDB()
	if err != nil {
		return err
	}
	sqlDB, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(context.Background(), cmd, sqlDB, ".")
}
