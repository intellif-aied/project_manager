package reportemail

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"
)

type ImportResult struct {
	DisplayName string
	Email       string
	UserID      string
	Status      string
	Applied     bool
}

type importUser struct {
	ID      string
	Aliases []string
}

func ImportRecipients(ctx context.Context, database *sql.DB, input io.Reader, apply bool) ([]ImportResult, error) {
	if database == nil || input == nil {
		return nil, errors.New("database and CSV input are required")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	usersByName, err := loadImportUsers(ctx, tx)
	if err != nil {
		return nil, err
	}
	existingEmails, err := loadRecipientEmails(ctx, tx)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(input)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read recipient CSV: %w", err)
	}
	if len(records) == 0 || len(records[0]) != 2 || strings.TrimSpace(records[0][0]) != "display_name" || strings.TrimSpace(records[0][1]) != "email" {
		return nil, errors.New("recipient CSV header must be display_name,email")
	}
	seenEmails := map[string]string{}
	results := make([]ImportResult, 0, len(records)-1)
	for rowIndex, record := range records[1:] {
		if len(record) != 2 {
			return nil, fmt.Errorf("recipient CSV row %d must have two columns", rowIndex+2)
		}
		result := ImportResult{DisplayName: strings.TrimSpace(record[0]), Email: strings.ToLower(strings.TrimSpace(record[1]))}
		parsed, parseErr := mail.ParseAddress(result.Email)
		if parseErr != nil || parsed.Address != result.Email {
			result.Status = "invalid_email"
			results = append(results, result)
			continue
		}
		matches := usersByName[result.DisplayName]
		switch len(matches) {
		case 0:
			result.Status = "not_found"
		case 1:
			result.UserID = matches[0].ID
		default:
			result.Status = "ambiguous"
		}
		if result.Status != "" {
			results = append(results, result)
			continue
		}
		if owner, exists := existingEmails[result.Email]; exists && owner != result.UserID {
			result.Status = "email_conflict"
			results = append(results, result)
			continue
		}
		if owner, exists := seenEmails[result.Email]; exists && owner != result.UserID {
			result.Status = "email_conflict"
			results = append(results, result)
			continue
		}
		seenEmails[result.Email] = result.UserID
		result.Status = "matched"
		if apply {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO report_email_recipients (user_id, email, source, enabled, verified_at)
				VALUES ($1::bigint, $2, 'enterprise_directory', true, now())
				ON CONFLICT (user_id) DO UPDATE SET
					email = EXCLUDED.email, source = EXCLUDED.source, enabled = true,
					verified_at = now(), updated_at = now()`, result.UserID, result.Email); err != nil {
				return nil, fmt.Errorf("apply recipient row %d: %w", rowIndex+2, err)
			}
			result.Applied = true
		}
		results = append(results, result)
	}
	if apply {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func loadImportUsers(ctx context.Context, tx *sql.Tx) (map[string][]importUser, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id::text, username, nickname, name FROM users WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byName := map[string][]importUser{}
	for rows.Next() {
		var user importUser
		var username, nickname, name string
		if err := rows.Scan(&user.ID, &username, &nickname, &name); err != nil {
			return nil, err
		}
		aliasSet := map[string]struct{}{}
		for _, alias := range []string{username, nickname, name} {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				aliasSet[alias] = struct{}{}
			}
		}
		for alias := range aliasSet {
			user.Aliases = append(user.Aliases, alias)
		}
		sort.Strings(user.Aliases)
		for _, alias := range user.Aliases {
			byName[alias] = append(byName[alias], user)
		}
	}
	return byName, rows.Err()
}

func loadRecipientEmails(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT lower(email), user_id::text FROM report_email_recipients`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := map[string]string{}
	for rows.Next() {
		var email, userID string
		if err := rows.Scan(&email, &userID); err != nil {
			return nil, err
		}
		owners[email] = userID
	}
	return owners, rows.Err()
}
