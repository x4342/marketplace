package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/x4342/marketplace/db/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := migrate(ctx); err != nil {
		// Driver errors can include connection credentials or database values.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			log.Fatalf("Database migration failed (SQLSTATE %s)", pgErr.Code)
		}
		log.Fatal("Database migration failed. Check database connectivity and configuration.")
	}
}

func migrate(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("missing DATABASE_URL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files,
		goose.WithSessionLocker(locker), goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return err
	}
	applied, err := provider.Up(ctx)
	for _, migration := range applied {
		if migration.Error == nil {
			log.Printf("Applied database migration %d", migration.Source.Version)
		}
	}
	if err != nil {
		return err
	}
	log.Print("Database schema is up to date")
	return nil
}
