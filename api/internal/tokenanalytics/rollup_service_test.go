package tokenanalytics

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

type beginOptionsDriver struct {
	options chan driver.TxOptions
}

func (d *beginOptionsDriver) Open(string) (driver.Conn, error) {
	return &beginOptionsConn{options: d.options}, nil
}

type beginOptionsConn struct {
	options chan driver.TxOptions
}

func (c *beginOptionsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *beginOptionsConn) Close() error { return nil }

func (c *beginOptionsConn) Begin() (driver.Tx, error) {
	return nil, errors.New("legacy begin is not supported")
}

func (c *beginOptionsConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.options <- options
	return nil, errors.New("stop after capturing transaction options")
}

func TestCreateSummaryUsesRepeatableReadSnapshotTransaction(t *testing.T) {
	options := make(chan driver.TxOptions, 1)
	driverName := fmt.Sprintf("tokenanalytics-begin-options-%p", options)
	sql.Register(driverName, &beginOptionsDriver{options: options})
	database, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	_, _ = NewService(database).CreateSummary(context.Background(), Actor{ID: 1, Role: "admin"}, Filters{
		Scope: "management",
		From:  "2026-07-24",
		To:    "2026-07-26",
	})
	transactionOptions := <-options
	if transactionOptions.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		t.Fatalf("isolation=%v, want repeatable read", transactionOptions.Isolation)
	}
	if transactionOptions.ReadOnly {
		t.Fatal("snapshot transaction must remain read-write")
	}
}

func TestCreateSummaryReturnsStableBusyErrorAfterSerializationRetries(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for attempt := 0; attempt < 4; attempt++ {
		mock.ExpectBegin().WillReturnError(&pq.Error{Code: "40001"})
	}

	_, err = NewService(database).CreateSummary(context.Background(), Actor{ID: 1, Role: "admin"}, Filters{
		Scope: "management",
		From:  "2026-07-24",
		To:    "2026-07-26",
	})
	if !errors.Is(err, ErrSnapshotBusy) {
		t.Fatalf("error=%v, want ErrSnapshotBusy", err)
	}
	if strings.Contains(err.Error(), "40001") || strings.Contains(err.Error(), "could not serialize") {
		t.Fatalf("database error leaked: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
