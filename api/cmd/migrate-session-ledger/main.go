package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aidashboard/api/config"
	"github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionmigration"
	"github.com/aidashboard/api/storage"
)

func main() {
	apply := flag.Bool("apply", false, "enqueue legacy raw logs into the V2 session ledger")
	limit := flag.Int("limit", 0, "maximum sessions to process; zero means all")
	userID := flag.Int64("user-id", 0, "only process one user ID; zero means all users")
	sessionRef := flag.String("session-ref", "", "only process one Session reference")
	timeout := flag.Duration("timeout", 24*time.Hour, "overall command timeout")
	flag.Parse()

	cfg := config.Load()
	if !cfg.MinioConfigured() {
		log.Fatal("MinIO is not configured")
	}
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	store, err := storage.NewMinioStorage(cfg)
	if err != nil {
		log.Fatal(err)
	}
	backfiller, err := sessionmigration.New(database, store)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := backfiller.Run(ctx, sessionmigration.Options{
		Apply: *apply, Limit: *limit, UserID: *userID, SessionRef: *sessionRef,
	})
	if err != nil {
		log.Fatal(err)
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(os.Stdout, string(encoded))
	if report.Failed > 0 {
		os.Exit(2)
	}
}
