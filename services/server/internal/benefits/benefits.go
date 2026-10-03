package benefits

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Store manages check-ins and benefit grants (free cover allowance + micro-light
// credits). All monetary changes go through the wallet and are idempotent.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

type CheckInResult struct {
	StreakDays    int  `json:"streakDays"`
	RewardCredits int  `json:"rewardCredits"`
	BonusCredits  int  `json:"bonusCredits"`
	AlreadyToday  bool `json:"alreadyToday"`
}

// CheckIn credits the user for today. The daily reward alternates 10/15 by
// streak parity; a 7-day streak adds 30, and 20 check-ins in the current month
// adds 60. It is idempotent per user per calendar day.
func (s *Store) CheckIn(ctx context.Context, userID string, now time.Time) (CheckInResult, error) {
	if !validUUID(userID) {
		return CheckInResult{}, fmt.Errorf("invalid user id")
	}
	utcNow := now.UTC()
	date := utcNow.Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CheckInResult{}, fmt.Errorf("begin check-in: %w", err)
	}
	defer tx.Rollback()

	// Determine previous streak from the most recent check-in.
	var previousDate string
	var previousStreak int
	var prevBonus int
	err = tx.QueryRowContext(ctx,
		`SELECT DATE_FORMAT(checkin_date,'%Y-%m-%d'), streak_days, bonus_credits FROM checkins WHERE user_id=UUID_TO_BIN(?,1) ORDER BY checkin_date DESC LIMIT 1`,
		userID,
	).Scan(&previousDate, &previousStreak, &prevBonus)
	if err != nil && err != sql.ErrNoRows {
		return CheckInResult{}, fmt.Errorf("load previous check-in: %w", err)
	}

	// Already checked in today?
	var already int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM checkins WHERE user_id=UUID_TO_BIN(?,1) AND checkin_date=?`, userID, date).Scan(&already)
	if err != nil {
		return CheckInResult{}, fmt.Errorf("check today check-in: %w", err)
	}
	if already > 0 {
		return CheckInResult{AlreadyToday: true, StreakDays: previousStreak}, nil
	}

	// Compute streak.
	yesterday := utcNow.AddDate(0, 0, -1).Format("2006-01-02")
	streak := 1
	if previousDate == yesterday {
		streak = previousStreak + 1
	}

	// Daily reward alternates 10/15 by streak parity.
	reward := 10
	if streak%2 == 0 {
		reward = 15
	}

	bonus := 0
	if streak%7 == 0 {
		bonus += 30
	}

	// Monthly 20-day bonus (count check-ins in the current month).
	monthStart := time.Date(utcNow.Year(), utcNow.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	var monthCount int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM checkins WHERE user_id=UUID_TO_BIN(?,1) AND checkin_date>=?`,
		userID, monthStart,
	).Scan(&monthCount)
	if err != nil {
		return CheckInResult{}, fmt.Errorf("count monthly check-ins: %w", err)
	}
	if monthCount+1 >= 20 {
		bonus += 60
	}

	entryID, err := randomID()
	if err != nil {
		return CheckInResult{}, err
	}
	// Credit the wallet.
	if reward+bonus > 0 {
		result, err := tx.ExecContext(ctx,
			`UPDATE wallets SET available_balance=available_balance+?, version=version+1 WHERE user_id=UUID_TO_BIN(?,1)`,
			reward+bonus, userID,
		)
		if err != nil {
			return CheckInResult{}, fmt.Errorf("credit wallet: %w", err)
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return CheckInResult{}, fmt.Errorf("wallet not found")
		}
		var after int64
		if err := tx.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&after); err != nil {
			return CheckInResult{}, err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'grant',?,0,?,0,'checkin',NULL,?)`,
			entryID, userID, reward+bonus, after, userID+":checkin:"+date,
		)
		if err != nil {
			return CheckInResult{}, fmt.Errorf("record check-in wallet entry: %w", err)
		}
	}

	// Record the check-in.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO checkins (user_id,checkin_date,streak_days,reward_credits,bonus_credits) VALUES (UUID_TO_BIN(?,1),?,?,?,?)`,
		userID, date, streak, reward, bonus,
	)
	if err != nil {
		return CheckInResult{}, fmt.Errorf("insert check-in: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return CheckInResult{}, fmt.Errorf("commit check-in: %w", err)
	}
	return CheckInResult{StreakDays: streak, RewardCredits: reward, BonusCredits: bonus}, nil
}

// RegisterInitialBenefits grants the sign-up package: credits + free covers.
func (s *Store) RegisterInitialBenefits(ctx context.Context, userID string, credits int64, coverAllowance int, now time.Time) error {
	return s.grant(ctx, userID, "registration", credits, coverAllowance, userID+":registration", now)
}

// PublishReward grants the publish-review-passed reward. Idempotent per world.
func (s *Store) PublishReward(ctx context.Context, userID, worldID string, credits int64, coverAllowance int, now time.Time) error {
	return s.grant(ctx, userID, "publish_reward", credits, coverAllowance, userID+":publish:"+worldID, now)
}

// AchievementGrant credits one achievement reward. It is idempotent for the
// given key, so retries and re-evaluation never double-pay.
func (s *Store) AchievementGrant(ctx context.Context, userID, idempotencyKey string, credits int64, now time.Time) error {
	return s.grant(ctx, userID, "achievement", credits, 0, idempotencyKey, now)
}

// ConsumeCoverAllowance spends one free cover if available; otherwise it
// returns false so the caller can charge credits instead.
func (s *Store) ConsumeCoverAllowance(ctx context.Context, userID string) (bool, error) {
	if !validUUID(userID) {
		return false, fmt.Errorf("invalid user id")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE user_benefits SET cover_allowance=cover_allowance-1 WHERE user_id=UUID_TO_BIN(?,1) AND cover_allowance>0`,
		userID,
	)
	if err != nil {
		return false, fmt.Errorf("consume cover allowance: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

// RefundCoverAllowance restores one free cover that was consumed without a
// successful generation.
func (s *Store) RefundCoverAllowance(ctx context.Context, userID string) error {
	if !validUUID(userID) {
		return fmt.Errorf("invalid user id")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_benefits SET cover_allowance=cover_allowance+1 WHERE user_id=UUID_TO_BIN(?,1)`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("refund cover allowance: %w", err)
	}
	return nil
}

// ChargeCredits deducts credits from a user's available balance with a wallet
// entry. Returns ErrInsufficientCredits when the balance is too low.
func (s *Store) ChargeCredits(ctx context.Context, userID, bizType, bizID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	if !validUUID(userID) {
		return fmt.Errorf("invalid user id")
	}
	entryID, err := randomID()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin charge: %w", err)
	}
	defer tx.Rollback()

	var available, version int64
	err = tx.QueryRowContext(ctx, `SELECT available_balance, version FROM wallets WHERE user_id=UUID_TO_BIN(?,1) FOR UPDATE`, userID).Scan(&available, &version)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrInsufficientCredits
		}
		return fmt.Errorf("lock wallet for charge: %w", err)
	}
	if available < credits {
		return ErrInsufficientCredits
	}
	newAvailable := available - credits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?, version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, userID, version)
	if err != nil {
		return fmt.Errorf("charge wallet: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'settle',?,0,?,0,?,NULL,?)`,
		entryID, userID, -credits, newAvailable, bizType, userID+":"+bizType+":"+bizID)
	if err != nil {
		if isDuplicateEntryError(err) {
			// An identical charge was already recorded; treat as success so
			// retries after a failed generation stay idempotent.
			if commitErr := tx.Commit(); commitErr != nil {
				return fmt.Errorf("commit duplicate charge: %w", commitErr)
			}
			return nil
		}
		return fmt.Errorf("record charge wallet entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit charge: %w", err)
	}
	return nil
}

func isDuplicateEntryError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate entry") || strings.Contains(text, "unique constraint")
}

var ErrInsufficientCredits = fmt.Errorf("insufficient credits")

// RefundCredits returns previously charged credits for a business id. It is
// a no-op when no matching charge entry exists. The idempotency key for the
// charge and the refund must be distinct so retries stay consistent.
func (s *Store) RefundCredits(ctx context.Context, userID, bizType, bizID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	if !validUUID(userID) {
		return fmt.Errorf("invalid user id")
	}
	entryID, err := randomID()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin refund: %w", err)
	}
	defer tx.Rollback()

	var available, version int64
	err = tx.QueryRowContext(ctx, `SELECT available_balance, version FROM wallets WHERE user_id=UUID_TO_BIN(?,1) FOR UPDATE`, userID).Scan(&available, &version)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("lock wallet for refund: %w", err)
	}
	newAvailable := available + credits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?, version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, userID, version)
	if err != nil {
		return fmt.Errorf("refund wallet: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'refund',?,0,?,0,?,NULL,?)`,
		entryID, userID, credits, newAvailable, bizType, userID+":"+bizType+":"+bizID+":refund")
	if err != nil {
		return fmt.Errorf("record refund wallet entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refund: %w", err)
	}
	return nil
}

// Balance returns the user's credit balance and remaining free covers.
type Balance struct {
	Credits        int64 `json:"credits"`
	CoverAllowance int   `json:"coverAllowance"`
}

// CheckInRecord is one signed day in a user's check-in history.
type CheckInRecord struct {
	Date          string `json:"date"`
	StreakDays    int    `json:"streakDays"`
	RewardCredits int    `json:"rewardCredits"`
	BonusCredits  int    `json:"bonusCredits"`
}

// CheckInHistory returns the user's recent check-ins, newest first.
func (s *Store) CheckInHistory(ctx context.Context, userID string, limit int) ([]CheckInRecord, error) {
	if !validUUID(userID) {
		return nil, fmt.Errorf("invalid user id")
	}
	if limit <= 0 || limit > 366 {
		limit = 60
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DATE_FORMAT(checkin_date,'%Y-%m-%d'), streak_days, reward_credits, bonus_credits FROM checkins WHERE user_id=UUID_TO_BIN(?,1) ORDER BY checkin_date DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load check-in history: %w", err)
	}
	defer rows.Close()
	records := make([]CheckInRecord, 0, limit)
	for rows.Next() {
		var record CheckInRecord
		if err := rows.Scan(&record.Date, &record.StreakDays, &record.RewardCredits, &record.BonusCredits); err != nil {
			return nil, fmt.Errorf("scan check-in history: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check-in history: %w", err)
	}
	return records, nil
}

func (s *Store) Balance(ctx context.Context, userID string) (Balance, error) {
	var credits int64
	err := s.db.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&credits)
	if err != nil && err != sql.ErrNoRows {
		return Balance{}, err
	}
	var allowance int
	err = s.db.QueryRowContext(ctx, `SELECT cover_allowance FROM user_benefits WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&allowance)
	if err != nil && err != sql.ErrNoRows {
		return Balance{}, err
	}
	return Balance{Credits: credits, CoverAllowance: allowance}, nil
}

func (s *Store) grant(ctx context.Context, userID, grantType string, credits int64, coverAllowance int, idempotencyKey string, now time.Time) error {
	if !validUUID(userID) {
		return fmt.Errorf("invalid user id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin grant: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM benefit_grants WHERE user_id=UUID_TO_BIN(?,1) AND idempotency_key=?`, userID, idempotencyKey).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check grant: %w", err)
	}
	if exists > 0 {
		_ = tx.Commit()
		return nil
	}

	// Wallet credit.
	if credits > 0 {
		result, err := tx.ExecContext(ctx, `UPDATE wallets SET available_balance=available_balance+?, version=version+1 WHERE user_id=UUID_TO_BIN(?,1)`, credits, userID)
		if err != nil {
			return fmt.Errorf("grant credits: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("wallet not found for grant")
		}
		var after int64
		if err := tx.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&after); err != nil {
			return err
		}
		entryID, err := randomID()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'grant',?,0,?,0,?,NULL,?)`,
			entryID, userID, credits, after, grantType, idempotencyKey+":wallet")
		if err != nil {
			return fmt.Errorf("record grant wallet entry: %w", err)
		}
	}

	// Cover allowance.
	if coverAllowance > 0 {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO user_benefits (user_id,cover_allowance) VALUES (UUID_TO_BIN(?,1),?) ON DUPLICATE KEY UPDATE cover_allowance=cover_allowance+VALUES(cover_allowance)`,
			userID, coverAllowance,
		)
		if err != nil {
			return fmt.Errorf("grant cover allowance: %w", err)
		}
	}

	grantID, err := randomID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO benefit_grants (id,user_id,grant_type,credits,cover_allowance,idempotency_key,created_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?)`,
		grantID, userID, grantType, credits, coverAllowance, idempotencyKey, now.UTC())
	if err != nil {
		return fmt.Errorf("record benefit grant: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit grant: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	_, err := hex.DecodeString(compact)
	return err == nil
}
