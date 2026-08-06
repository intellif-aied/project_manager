package reportemail

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	defaultPollInterval = time.Minute
	defaultLeaseTTL     = 2 * time.Minute
	defaultRetryDelay   = 5 * time.Minute
	defaultBatchSize    = 100
)

type Service struct {
	store      Store
	mailer     Mailer
	config     Config
	location   *time.Location
	sendHour   int
	sendMinute int
}

func NewService(store Store, mailer Mailer, config Config) (*Service, error) {
	config = normalizeConfig(config)
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load report email timezone: %w", err)
	}
	parsed, err := time.Parse("15:04", config.TimeOfDay)
	if err != nil {
		return nil, fmt.Errorf("parse report email time: %w", err)
	}
	if config.Enabled && (store == nil || mailer == nil || strings.TrimSpace(config.WorkerID) == "") {
		return nil, errors.New("enabled report email service requires store, mailer, and worker ID")
	}
	return &Service{store: store, mailer: mailer, config: config, location: location, sendHour: parsed.Hour(), sendMinute: parsed.Minute()}, nil
}

func normalizeConfig(config Config) Config {
	if strings.TrimSpace(config.Timezone) == "" {
		config.Timezone = "Asia/Shanghai"
	}
	if strings.TrimSpace(config.TimeOfDay) == "" {
		config.TimeOfDay = "08:00"
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = defaultRetryDelay
	}
	if config.BatchSize <= 0 {
		config.BatchSize = defaultBatchSize
	}
	return config
}

func (service *Service) Start(ctx context.Context) {
	if service == nil || !service.config.Enabled {
		return
	}
	go func() {
		run := func(now time.Time) {
			if err := service.RunOnce(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("daily report email worker failed: %v", err)
			}
		}
		run(time.Now())
		ticker := time.NewTicker(service.config.PollInterval)
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

func (service *Service) RunOnce(ctx context.Context, now time.Time) error {
	if service == nil || !service.config.Enabled {
		return nil
	}
	localNow := now.In(service.location)
	if localNow.Hour() < service.sendHour || (localNow.Hour() == service.sendHour && localNow.Minute() < service.sendMinute) {
		return nil
	}
	reportDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day()-1, 0, 0, 0, 0, service.location)
	if err := service.prepare(ctx, reportDate); err != nil {
		return err
	}
	for processed := 0; processed < service.config.BatchSize; processed++ {
		delivery, found, err := service.store.ClaimDelivery(ctx, now, service.config.WorkerID, service.config.LeaseTTL)
		if err != nil {
			return fmt.Errorf("claim report email delivery: %w", err)
		}
		if !found {
			break
		}
		message := Message{ID: delivery.ID, To: delivery.RecipientEmail, Subject: delivery.Subject, TextBody: delivery.TextBody, HTMLBody: delivery.HTMLBody}
		if err := service.mailer.Send(ctx, message); err != nil {
			if markErr := service.store.MarkFailed(ctx, delivery.ID, service.config.WorkerID, now, service.config.RetryDelay, err); markErr != nil {
				return fmt.Errorf("mark report email failed: %w", markErr)
			}
			continue
		}
		if err := service.store.MarkSent(ctx, delivery.ID, service.config.WorkerID, now); err != nil {
			return fmt.Errorf("mark report email sent: %w", err)
		}
	}
	return nil
}

func (service *Service) prepare(ctx context.Context, date time.Time) error {
	people, err := service.store.ListPersonalCandidates(ctx, date)
	if err != nil {
		return fmt.Errorf("list personal report email candidates: %w", err)
	}
	for _, person := range people {
		if err := service.store.CreateDelivery(ctx, personalDelivery(date, person)); err != nil {
			return fmt.Errorf("create personal report email delivery: %w", err)
		}
	}
	teams, err := service.store.ListTeamCandidates(ctx, date)
	if err != nil {
		return fmt.Errorf("list team report email candidates: %w", err)
	}
	for _, team := range teams {
		if err := service.store.CreateDelivery(ctx, teamDelivery(date, team)); err != nil {
			return fmt.Errorf("create team report email delivery: %w", err)
		}
	}
	return nil
}
