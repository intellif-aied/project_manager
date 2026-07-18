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
	"github.com/aidashboard/api/internal/contentinventory"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/storage"
)

func main() {
	action := flag.String("action", string(contentinventory.ActionPlan), "plan or scan")
	through := flag.String("snapshot-through-revision-id", "", "frozen upper revision ID returned by plan")
	after := flag.String("after-revision-id", "", "exclusive resume cursor returned by the previous scan")
	onlyRevision := flag.String("revision-id", "", "retry exactly one failed eligible revision")
	limit := flag.Int("limit", 0,
		fmt.Sprintf("revisions in this invocation (maximum %d)", contentinventory.MaximumRevisionsPerRun))
	maxBytes := flag.Int64("max-bytes", 1<<30,
		fmt.Sprintf("maximum indexed bytes in this invocation (hard maximum %d)", contentinventory.MaximumBytesPerRun))
	perRevisionTimeout := flag.Duration("per-revision-timeout", 5*time.Minute, "timeout for one revision object validation")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall command timeout")
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
	store, err := storage.NewMinioStorageReadOnly(cfg)
	if err != nil {
		log.Fatal(err)
	}
	reader, err := contentreader.New(database, store)
	if err != nil {
		log.Fatal(err)
	}
	auditor, err := contentinventory.New(database, reader)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	options := contentinventory.Options{
		Action: contentinventory.Action(*action), SnapshotThroughID: *through,
		AfterRevisionID: *after, OnlyRevisionID: *onlyRevision, Limit: *limit,
	}
	if options.Action == contentinventory.ActionScan {
		options.PerRevisionTimeout = *perRevisionTimeout
		options.MaxBytes = *maxBytes
	}
	report, err := auditor.Run(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, string(encoded))
	if report.FailedRevisions > 0 {
		os.Exit(2)
	}
}
