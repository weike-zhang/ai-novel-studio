package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, input CreateInput) (Report, error) {
	normalized, err := input.Normalized()
	if err != nil { return Report{}, err }
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil { return Report{}, fmt.Errorf("begin report: %w", err) }
	defer tx.Rollback()

	var worldStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM worlds WHERE id=UUID_TO_BIN(?,1) FOR UPDATE`, normalized.WorldID).Scan(&worldStatus)
	if errors.Is(err, sql.ErrNoRows) { return Report{}, ErrNotFound }
	if err != nil { return Report{}, fmt.Errorf("lock reported world: %w", err) }
	if worldStatus != "published" && worldStatus != "suspended" {
		return Report{}, ErrForbidden
	}

	var userStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=UUID_TO_BIN(?,1) FOR UPDATE`, normalized.ReporterID).Scan(&userStatus)
	if errors.Is(err, sql.ErrNoRows) { return Report{}, ErrForbidden }
	if err != nil { return Report{}, fmt.Errorf("lock reporter: %w", err) }
	if userStatus != "active" { return Report{}, ErrForbidden }

	reportID, err := newUUID()
	if err != nil { return Report{}, err }
	note := sql.NullString{String: normalized.Note, Valid: normalized.Note != ""}
	_, err = tx.ExecContext(ctx, `INSERT INTO world_reports (id, world_id, reporter_id, reason, note) VALUES (UUID_TO_BIN(?,1), UUID_TO_BIN(?,1), UUID_TO_BIN(?,1), ?, ?)`,
		reportID, normalized.WorldID, normalized.ReporterID, normalized.Reason, note)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return Report{}, ErrDuplicateReport
		}
		return Report{}, fmt.Errorf("insert world report: %w", err)
	}
	if err := tx.Commit(); err != nil { return Report{}, fmt.Errorf("commit report: %w", err) }

	report, err := s.getByID(ctx, reportID)
	if err != nil { return Report{}, err }
	return report, nil
}

func (s *Store) getByID(ctx context.Context, reportID string) (Report, error) {
	var report Report
	var note sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1), BIN_TO_UUID(world_id,1), BIN_TO_UUID(reporter_id,1), reason, note, status, created_at, updated_at FROM world_reports WHERE id=UUID_TO_BIN(?,1)`, reportID).
		Scan(&report.ID, &report.WorldID, &report.ReporterID, &report.Reason, &note, &report.Status, &report.CreatedAt, &report.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) { return Report{}, ErrNotFound }
	if err != nil { return Report{}, fmt.Errorf("load report: %w", err) }
	if note.Valid { report.Note = note.String }
	return report, nil
}
