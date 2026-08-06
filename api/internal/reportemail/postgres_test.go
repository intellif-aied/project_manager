package reportemail

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateDeliveryCanRefreshSkippedSnapshot(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := NewPostgresStore(database)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("INSERT INTO report_email_deliveries").
		WithArgs("2026-08-05", DeliveryPersonal, "1", "", "member@example.com", "subject", "text", "html", "pending", "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	err = store.CreateDelivery(context.Background(), Delivery{
		ReportDate: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), Type: DeliveryPersonal,
		RecipientUserID: "1", RecipientEmail: "member@example.com", Subject: "subject",
		TextBody: "text", HTMLBody: "html", Status: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimDeliveryReturnsNoWork(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, _ := NewPostgresStore(database)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, report_date, delivery_type").WithArgs(sqlmock.AnyArg()).WillReturnRows(
		sqlmock.NewRows([]string{"id", "report_date", "delivery_type", "recipient_user_id", "team_id", "recipient_email", "subject", "text_body", "html_body", "attempts"}),
	)
	mock.ExpectRollback()
	_, found, err := store.ClaimDelivery(context.Background(), time.Now(), "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("unexpected delivery")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
