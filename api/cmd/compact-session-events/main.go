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
	"github.com/aidashboard/api/internal/contentcompaction"
)

func main() {
	action := flag.String("action", string(contentcompaction.ActionPlan),
		"plan, copy, mirror, reconcile, verify, cutover, rollback, or finalize")
	apply := flag.Bool("apply", false, "allow the selected mutating action")
	batchSize := flag.Int("batch-size", 0,
		fmt.Sprintf("rows per batch (maximum %d)", contentcompaction.MaximumBatchSize))
	maxBatches := flag.Int("max-batches", 0,
		fmt.Sprintf("batches in this invocation (maximum %d)", contentcompaction.MaximumBatchesPerRun))
	expectedRows := flag.Int64("expected-source-rows", -1,
		"exact source row count required by cutover/rollback")
	confirmDrop := flag.String("confirm-drop", "",
		"must equal session_content_events_payload_archive for finalize")
	lockTimeout := flag.Duration("lock-timeout", 5*time.Second,
		"maximum wait for mirror/cutover/rollback/finalize table locks")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall command timeout")
	flag.Parse()

	cfg := config.Load()
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	compactor, err := contentcompaction.New(database)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := compactor.Run(ctx, contentcompaction.Options{
		Action: contentcompaction.Action(*action), Apply: *apply,
		BatchSize: *batchSize, MaxBatches: *maxBatches,
		ExpectedSourceRows: *expectedRows, ConfirmDrop: *confirmDrop,
		LockTimeout: *lockTimeout,
	})
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, string(encoded))
}
