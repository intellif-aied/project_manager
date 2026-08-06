package reportemail

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestImportRecipientsDryRunOnlyMatchesUniqueExactName(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, username, nickname, name FROM users").WillReturnRows(
		sqlmock.NewRows([]string{"id", "username", "nickname", "name"}).
			AddRow("1", "alice", "陈高华", "").
			AddRow("2", "bob", "重名", "").
			AddRow("3", "carol", "重名", ""),
	)
	mock.ExpectQuery("SELECT lower\\(email\\), user_id::text FROM report_email_recipients").WillReturnRows(
		sqlmock.NewRows([]string{"email", "user_id"}),
	)
	mock.ExpectRollback()
	input := "display_name,email\n陈高华,chen.gaohua@example.com\n重名,same@example.com\n不存在,missing@example.com\n"
	results, err := ImportRecipients(context.Background(), database, strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Status != "matched" || results[0].Applied || results[1].Status != "ambiguous" || results[2].Status != "not_found" {
		t.Fatalf("results = %#v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestImportRecipientsApplyWritesOnlyMatchedRows(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, username, nickname, name FROM users").WillReturnRows(
		sqlmock.NewRows([]string{"id", "username", "nickname", "name"}).AddRow("1", "alice", "陈高华", ""),
	)
	mock.ExpectQuery("SELECT lower\\(email\\), user_id::text FROM report_email_recipients").WillReturnRows(
		sqlmock.NewRows([]string{"email", "user_id"}),
	)
	mock.ExpectExec("INSERT INTO report_email_recipients").WithArgs("1", "chen.gaohua@example.com").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	results, err := ImportRecipients(context.Background(), database, strings.NewReader("display_name,email\n陈高华,chen.gaohua@example.com\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Applied {
		t.Fatalf("results = %#v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
