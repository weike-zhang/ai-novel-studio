package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const integrationAuthUserID = "0198b8a0-2001-7000-8000-000000000001"

func TestMySQLRepositoryLoginCreatesBinaryUUIDSession(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	passwords, err := newPasswordHasher(testArgonParams(), bytes.NewReader(bytes.Repeat([]byte{9}, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	secrets := testSecrets(t)
	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(repository, passwords, secrets, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	service.random = &countingReader{next: 17}
	service.now = func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }

	prepared, err := service.PrepareCredentials("Repo_User", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,password_hash,recovery_code_digest) VALUES (UUID_TO_BIN(?,1),?,?,?,?)`,
		integrationAuthUserID, "Repo_User", "合成账户", prepared.PasswordHash, prepared.RecoveryCodeDigest)
	if err != nil {
		t.Fatalf("seed auth user: %v", err)
	}

	login, err := service.Login(ctx, " repo_USER ", "correct horse 42")
	if err != nil {
		t.Fatalf("login through mysql repository: %v", err)
	}
	if !validUUID(login.Session.ID) || !validUUID(login.Session.FamilyID) {
		t.Fatalf("session UUIDs are invalid: id=%q family=%q", login.Session.ID, login.Session.FamilyID)
	}
	if login.Session.ID[14] != '4' || login.Session.FamilyID[14] != '4' {
		t.Fatalf("session UUIDs are not v4: id=%q family=%q", login.Session.ID, login.Session.FamilyID)
	}
	if login.Session.ID[19] != '8' && login.Session.ID[19] != '9' && login.Session.ID[19] != 'a' && login.Session.ID[19] != 'b' {
		t.Fatalf("session UUID variant is invalid: %q", login.Session.ID)
	}

	var storedID, storedFamily string
	var idBytes, familyBytes int
	err = db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(family_id,1),OCTET_LENGTH(id),OCTET_LENGTH(family_id) FROM auth_sessions WHERE token_digest=?`, login.Session.TokenDigest).Scan(
		&storedID, &storedFamily, &idBytes, &familyBytes,
	)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	if storedID != login.Session.ID || storedFamily != login.Session.FamilyID || idBytes != 16 || familyBytes != 16 {
		t.Fatalf("stored session mismatch: id=%q family=%q bytes=%d/%d", storedID, storedFamily, idBytes, familyBytes)
	}
	if _, err := service.AuthenticateSession(ctx, login.SessionToken); err != nil {
		t.Fatalf("authenticate persisted session: %v", err)
	}
}

func TestMySQLRepositorySessionsAndAtomicCredentialChanges(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	oldDigest := bytes.Repeat([]byte{1}, 32)
	_, err := db.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,password_hash,recovery_code_digest) VALUES (UUID_TO_BIN(?,1),'repo_methods','合成账户','old-hash',?)`, integrationAuthUserID, oldDigest)
	if err != nil {
		t.Fatalf("seed auth user: %v", err)
	}
	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	account, err := repository.FindAccountByNormalizedName(ctx, "repo_methods")
	if err != nil || account.ID != integrationAuthUserID || account.PasswordHash != "old-hash" {
		t.Fatalf("FindAccountByNormalizedName() = %#v, %v", account, err)
	}
	if _, err := repository.FindAccountByNormalizedName(ctx, "missing_account"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing account error = %v", err)
	}
	if err := repository.UpdatePasswordHash(ctx, integrationAuthUserID, "old-hash", "upgraded-hash"); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdatePasswordHash(ctx, integrationAuthUserID, "old-hash", "lost-update"); !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("stale password update error = %v", err)
	}

	now := time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)
	familyID := "0198b8a0-2001-4000-8000-000000000010"
	first := integrationSession("0198b8a0-2001-4000-8000-000000000011", familyID, 11, now)
	second := integrationSession("0198b8a0-2001-4000-8000-000000000012", familyID, 12, now)
	if err := repository.CreateSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.FindSessionByDigest(ctx, first.TokenDigest)
	if err != nil || loaded.ID != first.ID || loaded.FamilyID != familyID {
		t.Fatalf("FindSessionByDigest() = %#v, %v", loaded, err)
	}
	if err := repository.RotateSession(ctx, first.ID, second, now.Add(time.Hour), now.Add(time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repository.RotateSession(ctx, first.ID, integrationSession("0198b8a0-2001-4000-8000-000000000013", familyID, 13, now), now, now); !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("second rotation error = %v", err)
	}
	if err := repository.RevokeSessionByDigest(ctx, bytes.Repeat([]byte{99}, 32), now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown revoke error = %v", err)
	}
	if err := repository.RevokeSessionFamily(ctx, familyID, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, repository, second.TokenDigest)
	directSession := integrationSession("0198b8a0-2001-4000-8000-000000000014", "0198b8a0-2001-4000-8000-000000000015", 14, now)
	if err := repository.CreateSession(ctx, directSession); err != nil {
		t.Fatal(err)
	}
	if err := repository.RevokeSessionByDigest(ctx, directSession.TokenDigest, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, repository, directSession.TokenDigest)
	if err := repository.RevokeSessionByDigest(ctx, directSession.TokenDigest, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("repeat revoke should recognize the persisted session: %v", err)
	}

	changeSession := integrationSession("0198b8a0-2001-4000-8000-000000000021", "0198b8a0-2001-4000-8000-000000000020", 21, now)
	if err := repository.CreateSession(ctx, changeSession); err != nil {
		t.Fatal(err)
	}
	if err := repository.ChangePasswordAndRevokeSessions(ctx, integrationAuthUserID, "upgraded-hash", "changed-hash", now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, repository, changeSession.TokenDigest)

	recoverySession := integrationSession("0198b8a0-2001-4000-8000-000000000031", "0198b8a0-2001-4000-8000-000000000030", 31, now)
	if err := repository.CreateSession(ctx, recoverySession); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecoverPassword(ctx, integrationAuthUserID, oldDigest, "must-not-stick", bytes.Repeat([]byte{2}, 33), now.Add(4*time.Hour)); err == nil {
		t.Fatal("oversized recovery digest should fail")
	}
	assertUserCredentials(t, db, "changed-hash", oldDigest)
	assertSessionActive(t, repository, recoverySession.TokenDigest)

	newDigest := bytes.Repeat([]byte{3}, 32)
	if err := repository.RecoverPassword(ctx, integrationAuthUserID, oldDigest, "recovered-hash", newDigest, now.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertUserCredentials(t, db, "recovered-hash", newDigest)
	assertSessionRevoked(t, repository, recoverySession.TokenDigest)

	rotationSession := integrationSession("0198b8a0-2001-4000-8000-000000000041", "0198b8a0-2001-4000-8000-000000000040", 41, now)
	if err := repository.CreateSession(ctx, rotationSession); err != nil {
		t.Fatal(err)
	}
	rotatedDigest := bytes.Repeat([]byte{4}, 32)
	if err := repository.RotateRecoveryCode(ctx, integrationAuthUserID, "recovered-hash", rotatedDigest, now.Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertUserCredentials(t, db, "recovered-hash", rotatedDigest)
	assertSessionRevoked(t, repository, rotationSession.TokenDigest)
	logoutAllSession := integrationSession("0198b8a0-2001-4000-8000-000000000051", "0198b8a0-2001-4000-8000-000000000050", 51, now)
	if err := repository.CreateSession(ctx, logoutAllSession); err != nil {
		t.Fatal(err)
	}
	if err := repository.RevokeAllSessions(ctx, integrationAuthUserID, now.Add(7*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, repository, logoutAllSession.TokenDigest)
}

func TestMySQLRepositoryRegistrationCreatesUserWalletGrantAndSession(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	userID := "0198b8a0-2001-7000-8000-000000000061"
	session := integrationSessionForUser("0198b8a0-2001-4000-8000-000000000062", userID, "0198b8a0-2001-4000-8000-000000000060", 61, now)
	if err := repository.CreateRegisteredUser(ctx, CreateRegisteredUserInput{
		ID:                    userID,
		AccountName:           "Reg_User",
		AccountNameNormalized: "reg_user",
		Nickname:              "Reg_User",
		PasswordHash:          "hash",
		EmailNormalized:       "reg@example.com",
		EmailCiphertext:       []byte("sealed"),
		EmailVerifiedAt:       now,
		AdultConfirmedAt:      now,
		TermsVersion:          "v1",
		RecoveryCodeDigest:    bytes.Repeat([]byte{5}, 32),
		InitialLight:          20,
		InitialSession:        session,
	}); err != nil {
		t.Fatalf("CreateRegisteredUser() error = %v", err)
	}

	var available, reserved int64
	if err := db.QueryRowContext(ctx, `SELECT available_balance,reserved_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&available, &reserved); err != nil {
		t.Fatalf("load wallet: %v", err)
	}
	if available != 20 || reserved != 0 {
		t.Fatalf("wallet = (%d,%d), want (20,0)", available, reserved)
	}
	var entryType, idempotencyKey string
	var delta int64
	if err := db.QueryRowContext(ctx, `SELECT entry_type,available_delta,idempotency_key FROM wallet_entries WHERE user_id=UUID_TO_BIN(?,1) AND biz_type='initial_light'`, userID).Scan(&entryType, &delta, &idempotencyKey); err != nil {
		t.Fatalf("load grant entry: %v", err)
	}
	if entryType != "grant" || delta != 20 || idempotencyKey != userID+":initial-light" {
		t.Fatalf("grant entry = %q %d %q", entryType, delta, idempotencyKey)
	}
	var emailVerified, adultConfirmed *time.Time
	var ciphertext []byte
	var termsVersion string
	if err := db.QueryRowContext(ctx, `SELECT email_verified_at,adult_confirmed_at,email_ciphertext,terms_version FROM users WHERE id=UUID_TO_BIN(?,1)`, userID).Scan(&emailVerified, &adultConfirmed, &ciphertext, &termsVersion); err != nil {
		t.Fatalf("load user registration fields: %v", err)
	}
	if emailVerified == nil || adultConfirmed == nil || string(ciphertext) != "sealed" || termsVersion != "v1" {
		t.Fatalf("registration fields = verified %v confirmed %v cipher %q terms %q", emailVerified, adultConfirmed, ciphertext, termsVersion)
	}
	loaded, err := repository.FindSessionByDigest(ctx, session.TokenDigest)
	if err != nil || loaded.UserID != userID {
		t.Fatalf("FindSessionByDigest() = %#v, %v", loaded, err)
	}
}

func TestMySQLRepositoryRegistrationDuplicateEmailAndAccountName(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	first := integrationSessionForUser("0198b8a0-2001-4000-8000-000000000071", "0198b8a0-2001-7000-8000-000000000072", "0198b8a0-2001-4000-8000-000000000070", 71, now)
	if err := repository.CreateRegisteredUser(ctx, CreateRegisteredUserInput{
		ID:                    "0198b8a0-2001-7000-8000-000000000072",
		AccountName:           "Dup_User",
		AccountNameNormalized: "dup_user",
		Nickname:              "Dup_User",
		PasswordHash:          "hash",
		EmailNormalized:       "dup@example.com",
		EmailCiphertext:       []byte("sealed"),
		EmailVerifiedAt:       now,
		AdultConfirmedAt:      now,
		TermsVersion:          "v1",
		RecoveryCodeDigest:    bytes.Repeat([]byte{6}, 32),
		InitialLight:          0,
		InitialSession:        first,
	}); err != nil {
		t.Fatalf("first registration error = %v", err)
	}
	exists, err := repository.EmailExists(ctx, "dup@example.com")
	if err != nil || !exists {
		t.Fatalf("EmailExists() = %v %v", exists, err)
	}

	duplicateEmail := integrationSession("0198b8a0-2001-4000-8000-000000000081", "0198b8a0-2001-4000-8000-000000000080", 81, now)
	if err := repository.CreateRegisteredUser(ctx, CreateRegisteredUserInput{
		ID:                    "0198b8a0-2001-7000-8000-000000000082",
		AccountName:           "Other_01",
		AccountNameNormalized: "other_01",
		Nickname:              "Other_01",
		PasswordHash:          "hash",
		EmailNormalized:       "dup@example.com",
		EmailCiphertext:       []byte("sealed"),
		EmailVerifiedAt:       now,
		AdultConfirmedAt:      now,
		TermsVersion:          "v1",
		RecoveryCodeDigest:    bytes.Repeat([]byte{7}, 32),
		InitialLight:          0,
		InitialSession:        duplicateEmail,
	}); !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Fatalf("duplicate email error = %v", err)
	}

	duplicateName := integrationSession("0198b8a0-2001-4000-8000-000000000091", "0198b8a0-2001-4000-8000-000000000090", 91, now)
	if err := repository.CreateRegisteredUser(ctx, CreateRegisteredUserInput{
		ID:                    "0198b8a0-2001-7000-8000-000000000092",
		AccountName:           "dup_user",
		AccountNameNormalized: "dup_user",
		Nickname:              "Dup_User",
		PasswordHash:          "hash",
		EmailNormalized:       "fresh@example.com",
		EmailCiphertext:       []byte("sealed"),
		EmailVerifiedAt:       now,
		AdultConfirmedAt:      now,
		TermsVersion:          "v1",
		RecoveryCodeDigest:    bytes.Repeat([]byte{8}, 32),
		InitialLight:          0,
		InitialSession:        duplicateName,
	}); !errors.Is(err, ErrAccountNameTaken) {
		t.Fatalf("duplicate account error = %v", err)
	}
}

func TestMySQLRepositoryRegistrationRiskEventsAndCounts(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	cookieHash := bytes.Repeat([]byte{9}, 32)
	for index := 0; index < 2; index++ {
		if err := repository.CreateRegistrationRiskEvent(ctx, RegistrationRiskEvent{
			EmailHash:       bytes.Repeat([]byte{byte(index)}, 32),
			IPPrefixHash:    bytes.Repeat([]byte{10}, 32),
			RiskCookieHash:  cookieHash,
			ASN:             "",
			TurnstileResult: "success",
			RiskLevel:       "low",
			Action:          "allow",
			GrantGranted:    true,
			CreatedAt:       now.Add(-time.Duration(index) * time.Hour),
		}); err != nil {
			t.Fatalf("CreateRegistrationRiskEvent(%d) error = %v", index, err)
		}
	}
	count, err := repository.CountRegistrationsByRiskCookie(ctx, cookieHash, now.Add(-24*time.Hour))
	if err != nil || count != 2 {
		t.Fatalf("CountRegistrationsByRiskCookie() = %d %v", count, err)
	}
	old, err := repository.CountRegistrationsByRiskCookie(ctx, cookieHash, now.Add(-48*time.Hour))
	if err != nil || old != 2 {
		t.Fatalf("CountRegistrationsByRiskCookie(window) = %d %v", old, err)
	}

	if err := repository.CreateEmailDeliveryLog(ctx, EmailDeliveryLog{
		EmailHash:     bytes.Repeat([]byte{11}, 32),
		Purpose:       "registration",
		ProviderMsgID: "provider-1",
		Accepted:      true,
		Result:        "",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("CreateEmailDeliveryLog() error = %v", err)
	}
	var logged int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_delivery_logs`).Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("email delivery logs = %d %v", logged, err)
	}
}

func TestMySQLRepositoryMeSessionsAndRevokeByID(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	userID := integrationAuthUserID
	_, err = db.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,password_hash) VALUES (UUID_TO_BIN(?,1),'repo_sessions','合成账户','hash')`, userID)
	if err != nil {
		t.Fatalf("seed sessions user: %v", err)
	}
	sessionOne := integrationSession("0198b8a0-2001-4000-8000-0000000000a1", "0198b8a0-2001-4000-8000-0000000000a0", 101, now)
	sessionTwo := integrationSession("0198b8a0-2001-4000-8000-0000000000a2", "0198b8a0-2001-4000-8000-0000000000a1", 102, now.Add(time.Hour))
	if err := repository.CreateSession(ctx, sessionOne); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateSession(ctx, sessionTwo); err != nil {
		t.Fatal(err)
	}
	sessions, err := repository.ListSessions(ctx, userID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("ListSessions() = %d sessions, %v", len(sessions), err)
	}
	account, err := repository.FindAccountByID(ctx, userID)
	if err != nil || account.ID != userID {
		t.Fatalf("FindAccountByID() = %#v, %v", account, err)
	}
	balance, err := repository.WalletBalance(ctx, userID)
	if err != nil {
		t.Fatalf("WalletBalance() error = %v", err)
	}
	if balance != 0 {
		t.Fatalf("WalletBalance() = %d", balance)
	}
	if err := repository.RevokeSessionByID(ctx, sessionOne.ID, userID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("RevokeSessionByID() error = %v", err)
	}
	all, err := repository.ListSessions(ctx, userID)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListSessions() after revoke = %d sessions, %v", len(all), err)
	}
	loadedOne, err := repository.FindSessionByDigest(ctx, sessionOne.TokenDigest)
	if err != nil || loadedOne.RevokedAt == nil {
		t.Fatalf("revoked session still active: %#v, %v", loadedOne, err)
	}
	loadedTwo, err := repository.FindSessionByDigest(ctx, sessionTwo.TokenDigest)
	if err != nil || loadedTwo.RevokedAt != nil {
		t.Fatalf("unrelated session revoked: %#v, %v", loadedTwo, err)
	}
}

func TestMySQLRepositoryChangePasswordKeepsCurrentSession(t *testing.T) {
	db := openAuthTestDB(t)
	ctx := context.Background()
	cleanupAuthFixture(t, db)
	t.Cleanup(func() { cleanupAuthFixture(t, db) })

	repository, err := NewMySQLRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	userID := integrationAuthUserID
	oldDigest := bytes.Repeat([]byte{1}, 32)
	_, err = db.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,password_hash,recovery_code_digest) VALUES (UUID_TO_BIN(?,1),'repo_me','合成账户','old-hash',?)`, userID, oldDigest)
	if err != nil {
		t.Fatalf("seed me user: %v", err)
	}
	keep := integrationSession("0198b8a0-2001-4000-8000-0000000000b1", "0198b8a0-2001-4000-8000-0000000000b0", 111, now)
	revoke := integrationSession("0198b8a0-2001-4000-8000-0000000000b2", "0198b8a0-2001-4000-8000-0000000000b0", 112, now)
	if err := repository.CreateSession(ctx, keep); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateSession(ctx, revoke); err != nil {
		t.Fatal(err)
	}
	if err := repository.ChangePasswordAndRevokeOtherSessions(ctx, userID, "old-hash", "new-hash", keep.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("ChangePasswordAndRevokeOtherSessions() error = %v", err)
	}
	assertSessionActive(t, repository, keep.TokenDigest)
	assertSessionRevoked(t, repository, revoke.TokenDigest)
	assertUserCredentials(t, db, "new-hash", bytes.Repeat([]byte{1}, 32))
}

func openAuthTestDB(t *testing.T) *sql.DB {
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
		t.Fatalf("ping test mysql: %v", err)
	}
	return db
}

func cleanupAuthFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DELETE FROM email_delivery_logs`); err != nil {
		t.Fatalf("cleanup email delivery logs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM registration_risk_events`); err != nil {
		t.Fatalf("cleanup registration risk events: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup auth sessions: %v", err)
	}
	for _, id := range []string{
		"0198b8a0-2001-7000-8000-000000000061",
		"0198b8a0-2001-7000-8000-000000000072",
		"0198b8a0-2001-7000-8000-000000000082",
		"0198b8a0-2001-7000-8000-000000000092",
	} {
		if _, err := db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup registration session %s: %v", id, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM wallet_entries WHERE user_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup registration wallet entries %s: %v", id, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup registration wallet %s: %v", id, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup registration user %s: %v", id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE wda FROM world_draft_attempts wda JOIN world_draft_jobs wdj ON wdj.id=wda.job_id WHERE wdj.user_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup world draft attempts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM world_draft_jobs WHERE user_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup world draft jobs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE md FROM moderation_decisions md JOIN worlds w ON w.id=md.content_id WHERE w.owner_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup moderation decisions: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM world_reports WHERE reporter_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup world reports: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM worlds WHERE owner_id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup authored worlds: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id=UUID_TO_BIN(?,1)`, integrationAuthUserID); err != nil {
		t.Fatalf("cleanup auth user: %v", err)
	}
}

func integrationSession(id, familyID string, digestByte byte, now time.Time) Session {
	return Session{ID: id, UserID: integrationAuthUserID, FamilyID: familyID, TokenDigest: bytes.Repeat([]byte{digestByte}, 32), CreatedAt: now, LastUsedAt: now}
}

func integrationSessionForUser(id, userID, familyID string, digestByte byte, now time.Time) Session {
	return Session{ID: id, UserID: userID, FamilyID: familyID, TokenDigest: bytes.Repeat([]byte{digestByte}, 32), CreatedAt: now, LastUsedAt: now}
}

func assertUserCredentials(t *testing.T, db *sql.DB, wantHash string, wantDigest []byte) {
	t.Helper()
	var hash string
	var digest []byte
	if err := db.QueryRow(`SELECT password_hash,recovery_code_digest FROM users WHERE id=UUID_TO_BIN(?,1)`, integrationAuthUserID).Scan(&hash, &digest); err != nil {
		t.Fatal(err)
	}
	if hash != wantHash || !bytes.Equal(digest, wantDigest) {
		t.Fatalf("credentials = hash %q digest %x, want %q %x", hash, digest, wantHash, wantDigest)
	}
}

func assertSessionActive(t *testing.T, repository *MySQLRepository, digest []byte) {
	t.Helper()
	session, err := repository.FindSessionByDigest(context.Background(), digest)
	if err != nil || session.RevokedAt != nil {
		t.Fatalf("session should be active: %#v, %v", session, err)
	}
}

func assertSessionRevoked(t *testing.T, repository *MySQLRepository, digest []byte) {
	t.Helper()
	session, err := repository.FindSessionByDigest(context.Background(), digest)
	if err != nil || session.RevokedAt == nil {
		t.Fatalf("session should be revoked: %#v, %v", session, err)
	}
}
