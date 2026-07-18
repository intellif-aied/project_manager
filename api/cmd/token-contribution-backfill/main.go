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
	"github.com/aidashboard/api/internal/usage"
	"github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "enqueue v5 Contribution rebuilds; without this flag only inspect")
	batchSize := flag.Int("batch-size", 1, "maximum sources enqueued per short transaction batch")
	maxBatches := flag.Int("max-batches", 1, "maximum batches per invocation; zero means until no source remains")
	pause := flag.Duration("pause", time.Second, "minimum delay between batches or pressure checks")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall command timeout")
	repairSourceID := flag.String("repair-source-id", "", "reset one failed v5 source's derived rows and enqueue a targeted rebuild; requires --apply")
	flag.Parse()
	if *batchSize <= 0 || *maxBatches < 0 || *pause < 0 || *timeout <= 0 {
		log.Fatal("invalid token contribution backfill options")
	}

	cfg := config.Load()
	normalizerVersion, err := usage.NormalizerRevision(cfg.ClaudeCacheWriteVariant)
	if err != nil {
		log.Fatal(err)
	}
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *repairSourceID != "" {
		if !*apply {
			encoded, _ := json.MarshalIndent(map[string]any{
				"repair_source_id": *repairSourceID,
				"apply_required":   true,
			}, "", "  ")
			fmt.Fprintln(os.Stdout, string(encoded))
			return
		}
		report, err := usage.RepairFailedContributionRevision(ctx, database, normalizerVersion, *repairSourceID)
		if err != nil {
			log.Fatal(err)
		}
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(os.Stdout, string(encoded))
		return
	}

	startedAt := time.Now()
	before, err := usage.InspectContributionBackfill(ctx, database, normalizerVersion)
	if err != nil {
		log.Fatal(err)
	}
	report := usage.ContributionBackfillReport{Before: before, After: before}
	if *apply {
		for *maxBatches == 0 || report.Batches < *maxBatches {
			busy, err := usage.ContributionBackfillForegroundBusy(ctx, database)
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
			count, err := usage.EnqueueContributionBackfillBatch(ctx, database, normalizerVersion, *batchSize)
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
			report.EnqueuedSources += count
			report.Batches++
			if err := wait(ctx, *pause); err != nil {
				log.Fatal(err)
			}
		}
		report.After, err = usage.InspectContributionBackfill(ctx, database, normalizerVersion)
		if err != nil {
			log.Fatal(err)
		}
	}
	report.Complete = report.After.UnsafeSources == 0 && report.After.ActiveRevisions == report.After.EligibleSources &&
		report.After.MissingRevisions == 0 && report.After.BuildingRevisions == 0 &&
		report.After.FailedRevisions == 0 && report.After.PendingJobs == 0 && report.After.DeadJobs == 0 &&
		report.After.MissingFamilyMemberships == 0 && report.After.ReconciliationFailures == 0
	report.Elapsed = time.Since(startedAt).Round(time.Millisecond).String()
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(os.Stdout, string(encoded))
	if *apply && (report.After.FailedRevisions > 0 || report.After.DeadJobs > 0 ||
		report.After.ReconciliationFailures > 0) {
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
