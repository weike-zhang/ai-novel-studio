package benefits

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const testUserID = "018f2000-0000-7000-8000-000000000010"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

func cleanup(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db.ExecContext(ctx, `DELETE FROM checkins WHERE user_id=UUID_TO_BIN(?,1)`, testUserID)
	db.ExecContext(ctx, `DELETE FROM benefit_grants WHERE user_id=UUID_TO_BIN(?,1)`, testUserID)
	db.ExecContext(ctx, `DELETE FROM wallet_entries WHERE user_id=UUID_TO_BIN(?,1)`, testUserID)
	db.ExecContext(ctx, `DELETE FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, testUserID)
	db.ExecContext(ctx, `DELETE FROM user_benefits WHERE user_id=UUID_TO_BIN(?,1)`, testUserID)
	db.ExecContext(ctx, `DELETE FROM users WHERE id=UUID_TO_BIN(?,1)`, testUserID)
}

func seedUser(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'benefit_test','测试')`,
		testUserID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO wallets (user_id,available_balance,reserved_balance,version) VALUES (UUID_TO_BIN(?,1),0,0,1)`,
		testUserID)
	if err != nil {
		t.Fatal(err)
	}
}

func balance(t *testing.T, db *sql.DB) int64 {
	var available int64
	if err := db.QueryRow(`SELECT available_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, testUserID).Scan(&available); err != nil {
		t.Fatal(err)
	}
	return available
}

func coverAllowance(t *testing.T, db *sql.DB) int {
	var allowance int
	err := db.QueryRow(`SELECT cover_allowance FROM user_benefits WHERE user_id=UUID_TO_BIN(?,1)`, testUserID).Scan(&allowance)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return allowance
}

func TestCheckInDailyRewardAndBonus(t *testing.T) {
	db := openTestDB(t)
	cleanup(t, db)
	t.Cleanup(func() { cleanup(t, db) })
	seedUser(t, db)
	store := NewStore(db)
	ctx := context.Background()

	// Day 1: streak=1 -> reward 10
	result, err := store.CheckIn(ctx, testUserID, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	if err != nil || result.StreakDays != 1 || result.RewardCredits != 10 {
		t.Fatalf("day1 = %#v err=%v", result, err)
	}
	if got := balance(t, db); got != 10 {
		t.Fatalf("day1 balance=%d", got)
	}

	// Day 2 (consecutive): streak=2 -> reward 15
	result, err = store.CheckIn(ctx, testUserID, time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	if err != nil || result.StreakDays != 2 || result.RewardCredits != 15 {
		t.Fatalf("day2 = %#v err=%v", result, err)
	}
	if got := balance(t, db); got != 25 {
		t.Fatalf("day2 balance=%d", got)
	}

	// Same day again: idempotent, no double reward.
	result, err = store.CheckIn(ctx, testUserID, time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	if err != nil || !result.AlreadyToday {
		t.Fatalf("repeat = %#v err=%v", result, err)
	}
	if got := balance(t, db); got != 25 {
		t.Fatalf("repeat balance=%d", got)
	}

	// Days 3-19 are existing check-ins, so day 20 must receive the monthly bonus.
	for day := 3; day <= 19; day++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO checkins (user_id,checkin_date,streak_days,reward_credits,bonus_credits) VALUES (UUID_TO_BIN(?,1),?,?,?,0)`,
			testUserID, time.Date(2026, 8, day, 10, 0, 0, 0, time.UTC), day, 10,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err = store.CheckIn(ctx, testUserID, time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	if err != nil || result.StreakDays != 20 || result.BonusCredits != 60 {
		t.Fatalf("monthly bonus = %#v err=%v", result, err)
	}
	if got := balance(t, db); got != 100 {
		t.Fatalf("monthly bonus balance=%d", got)
	}
}

func TestRegisterAndPublishGrants(t *testing.T) {
	db := openTestDB(t)
	cleanup(t, db)
	t.Cleanup(func() { cleanup(t, db) })
	seedUser(t, db)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	if err := store.RegisterInitialBenefits(ctx, testUserID, 100, 3, now); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, db); got != 100 {
		t.Fatalf("register credits=%d", got)
	}
	if got := coverAllowance(t, db); got != 3 {
		t.Fatalf("register covers=%d", got)
	}
	// Idempotent replay.
	if err := store.RegisterInitialBenefits(ctx, testUserID, 100, 3, now); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, db); got != 100 {
		t.Fatalf("replay credits=%d", got)
	}

	// Publish reward.
	if err := store.PublishReward(ctx, testUserID, "0198b8a0-2001-4000-8000-000000000001", 90, 3, now); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, db); got != 190 {
		t.Fatalf("publish credits=%d", got)
	}
	if got := coverAllowance(t, db); got != 6 {
		t.Fatalf("publish covers=%d", got)
	}
}

func TestConsumeCoverAllowanceAndCharge(t *testing.T) {
	db := openTestDB(t)
	cleanup(t, db)
	t.Cleanup(func() { cleanup(t, db) })
	seedUser(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if err := store.RegisterInitialBenefits(ctx, testUserID, 20, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	// First cover free.
	free, err := store.ConsumeCoverAllowance(ctx, testUserID)
	if err != nil || !free {
		t.Fatalf("free cover = %v err=%v", free, err)
	}
	if got := coverAllowance(t, db); got != 0 {
		t.Fatalf("after free covers=%d", got)
	}
	// No allowance left -> must charge.
	free, err = store.ConsumeCoverAllowance(ctx, testUserID)
	if err != nil || free {
		t.Fatalf("second cover should not be free: %v %v", free, err)
	}
	if err := store.ChargeCredits(ctx, testUserID, "cover", "world-1", 8); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, db); got != 12 {
		t.Fatalf("after charge credits=%d", got)
	}
	// Insufficient.
	if err := store.ChargeCredits(ctx, testUserID, "cover", "world-2", 100); err != ErrInsufficientCredits {
		t.Fatalf("insufficient err = %v", err)
	}
}
