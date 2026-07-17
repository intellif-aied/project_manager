package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aidashboard/api/config"
	"github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "write catalog rows; without this flag only print current status")
	batchSize := flag.Int("batch-size", 10, "maximum slices per short transaction")
	maxBatches := flag.Int("max-batches", 1, "maximum batches per invocation; zero means until complete")
	pause := flag.Duration("pause", time.Second, "minimum delay between batches or pressure checks")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall command timeout")
	flag.Parse()
	if *batchSize <= 0 || *maxBatches < 0 || *pause < 0 || *timeout <= 0 {
		log.Fatal("invalid backfill options")
	}

	cfg := config.Load()
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	startedAt := time.Now()
	before, err := reportsourcecatalog.InspectBackfill(ctx, database)
	if err != nil {
		log.Fatal(err)
	}
	report := reportsourcecatalog.BackfillReport{Before: before, After: before}
	if *apply {
		for *maxBatches == 0 || report.Batches < *maxBatches {
			busy, err := reportsourcecatalog.ForegroundBusy(ctx, database)
			if err != nil {
				log.Fatal(err)
			}
			if busy {
				report.PressurePauses++
				if err := wait(ctx, *pause); err != nil {
					log.Fatal(err)
				}
				continue
			}
			count, err := reportsourcecatalog.RunBackfillBatch(ctx, database, *batchSize)
			if err != nil {
				if retryableDatabasePressure(err) {
					report.PressurePauses++
					if err := wait(ctx, *pause); err != nil {
						log.Fatal(err)
					}
					continue
				}
				log.Fatal(err)
			}
			if count == 0 {
				break
			}
			report.Processed += count
			report.Batches++
			if err := wait(ctx, *pause); err != nil {
				log.Fatal(err)
			}
		}
		report.After, err = reportsourcecatalog.InspectBackfill(ctx, database)
		if err != nil {
			log.Fatal(err)
		}
	}
	report.Complete = report.After.Missing == 0 && report.After.Building == 0 && report.After.Failed == 0
	report.Elapsed = time.Since(startedAt).Round(time.Millisecond)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(os.Stdout, string(encoded))
	if *apply && report.After.Failed > 0 {
		os.Exit(2)
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	if duration == 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableDatabasePressure(err error) bool {
	var pqError *pq.Error
	if !errors.As(err, &pqError) {
		return false
	}
	return pqError.Code == "55P03" || pqError.Code == "57014"
}
