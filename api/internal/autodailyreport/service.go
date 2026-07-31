package autodailyreport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
)

const (
	defaultPollInterval = 15 * time.Second
	defaultLeaseTTL     = 2 * time.Minute
	defaultClaimBatch   = 20
)

type Service struct {
	repository   repository
	submitter    Submitter
	workerID     string
	quietPeriod  time.Duration
	pollInterval time.Duration
	leaseTTL     time.Duration
	claimBatch   int
}

func NewService(database *sql.DB, submitter Submitter, workerID string) (*Service, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return newService(newPostgresRepository(database), submitter, workerID)
}

func newService(repo repository, submitter Submitter, workerID string) (*Service, error) {
	if repo == nil || submitter == nil || strings.TrimSpace(workerID) == "" {
		return nil, errors.New("repository, submitter, and worker ID are required")
	}
	return &Service{
		repository: repo, submitter: submitter, workerID: strings.TrimSpace(workerID),
		quietPeriod: QuietPeriod, pollInterval: defaultPollInterval,
		leaseTTL: defaultLeaseTTL, claimBatch: defaultClaimBatch,
	}, nil
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		run := func(now time.Time) {
			if err := s.RunOnce(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("auto daily report scheduler failed: %v", err)
			}
		}
		run(time.Now())
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				run(now)
			}
		}
	}()
}

func (s *Service) GetConfig(ctx context.Context) (Config, error) {
	config, err := s.repository.GetConfig(ctx)
	config.QuietPeriodSeconds = int(s.quietPeriod.Seconds())
	return config, err
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool, operatorID string) (Config, error) {
	config, err := s.repository.SetEnabled(ctx, enabled, strings.TrimSpace(operatorID), time.Now())
	config.QuietPeriodSeconds = int(s.quietPeriod.Seconds())
	return config, err
}

func (s *Service) RunOnce(ctx context.Context, now time.Time) error {
	config, err := s.repository.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := s.repository.ReconcileRuns(ctx, config.Enabled, now, s.quietPeriod); err != nil {
		return fmt.Errorf("reconcile runs: %w", err)
	}
	if !config.Enabled || config.EnabledSince == nil {
		if err := s.repository.SuppressPending(ctx); err != nil {
			return fmt.Errorf("suppress pending states: %w", err)
		}
		return nil
	}

	reportDate := biztime.Date(now)
	snapshots, err := s.repository.DiscoverSourceSnapshots(ctx, reportDate, *config.EnabledSince)
	if err != nil {
		return fmt.Errorf("discover report sources: %w", err)
	}
	for _, snapshot := range snapshots {
		if err := s.repository.ObserveSourceSnapshot(ctx, snapshot, s.quietPeriod); err != nil {
			return fmt.Errorf("observe sources for user %s: %w", snapshot.UserID, err)
		}
	}

	for claimed := 0; claimed < s.claimBatch; claimed++ {
		job, found, err := s.repository.ClaimDue(ctx, now, s.workerID, s.leaseTTL)
		if err != nil {
			return fmt.Errorf("claim due state: %w", err)
		}
		if !found {
			return nil
		}
		eligibility, err := s.repository.LoadReportEligibility(ctx, job)
		if err != nil {
			if markErr := s.repository.MarkSubmissionFailed(ctx, job, err.Error(), now, s.quietPeriod); markErr != nil {
				return fmt.Errorf("record report eligibility failure: %w", markErr)
			}
			continue
		}
		if !eligibility.Allowed {
			if err := s.repository.MarkBlocked(ctx, job, eligibility.Reason); err != nil {
				return fmt.Errorf("block protected report state: %w", err)
			}
			continue
		}
		runID, err := s.submitter.SubmitAutoDailyReport(ctx, SubmissionRequest{
			UserID: job.UserID, ReportDate: job.ReportDate,
			SourceFingerprint: job.SourceFingerprint,
			SourceSliceKeys:   append([]string(nil), job.SourceSliceKeys...),
			Guard:             eligibility.Guard,
		})
		if err != nil || strings.TrimSpace(runID) == "" {
			message := "submitter returned an empty run ID"
			if err != nil {
				message = err.Error()
			}
			if markErr := s.repository.MarkSubmissionFailed(ctx, job, message, now, s.quietPeriod); markErr != nil {
				return fmt.Errorf("record submission failure: %w", markErr)
			}
			continue
		}
		if err := s.repository.MarkRunning(ctx, job, strings.TrimSpace(runID)); err != nil {
			return fmt.Errorf("attach automatic report run: %w", err)
		}
	}
	return nil
}
