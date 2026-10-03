package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type MySQLRepository struct {
	db *sql.DB
}

var _ Repository = (*MySQLRepository)(nil)

func NewMySQLRepository(db *sql.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, ErrInvalidConfiguration
	}
	return &MySQLRepository{db: db}, nil
}

func (r *MySQLRepository) FindAccountByNormalizedName(ctx context.Context, normalized string) (Account, error) {
	var account Account
	err := r.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),account_name,account_name_normalized,COALESCE(password_hash,''),recovery_code_digest,status,COALESCE(email_normalized,''),email_ciphertext,email_verified_at,adult_confirmed_at,COALESCE(terms_version,''),created_at FROM users WHERE account_name_normalized=?`, normalized).Scan(
		&account.ID, &account.AccountName, &account.AccountNameNormalized, &account.PasswordHash, &account.RecoveryCodeDigest, &account.Status, &account.EmailNormalized, &account.EmailCiphertext, &account.EmailVerifiedAt, &account.AdultConfirmedAt, &account.TermsVersion, &account.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("find account: %w", err)
	}
	return account, nil
}

func (r *MySQLRepository) FindAccountByID(ctx context.Context, userID string) (Account, error) {
	if !validUUID(userID) {
		return Account{}, ErrAccountNotFound
	}
	var account Account
	err := r.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),account_name,account_name_normalized,COALESCE(password_hash,''),recovery_code_digest,status,COALESCE(email_normalized,''),email_ciphertext,email_verified_at,adult_confirmed_at,COALESCE(terms_version,''),created_at FROM users WHERE id=UUID_TO_BIN(?,1)`, userID).Scan(
		&account.ID, &account.AccountName, &account.AccountNameNormalized, &account.PasswordHash, &account.RecoveryCodeDigest, &account.Status, &account.EmailNormalized, &account.EmailCiphertext, &account.EmailVerifiedAt, &account.AdultConfirmedAt, &account.TermsVersion, &account.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("find account by id: %w", err)
	}
	return account, nil
}

func (r *MySQLRepository) UpdatePasswordHash(ctx context.Context, userID, expectedHash, replacementHash string) error {
	if !validUUID(userID) {
		return ErrConcurrentUpdate
	}
	result, err := r.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=UUID_TO_BIN(?,1) AND password_hash=?`, replacementHash, userID, expectedHash)
	return expectOne(result, err, "update password hash")
}

func (r *MySQLRepository) CreateSession(ctx context.Context, session Session) error {
	if !validUUID(session.ID) || !validUUID(session.UserID) || !validUUID(session.FamilyID) || len(session.TokenDigest) != 32 {
		return fmt.Errorf("create session: invalid session data")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_sessions (id,user_id,family_id,token_digest,created_at,last_used_at,expires_at,rotated_at,rotation_grace_ends_at,revoked_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?,?,?)`,
		session.ID, session.UserID, session.FamilyID, session.TokenDigest, session.CreatedAt.UTC(), session.LastUsedAt.UTC(), session.ExpiresAt, session.RotatedAt, session.RotationGraceEnds, session.RevokedAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *MySQLRepository) FindSessionByDigest(ctx context.Context, digest []byte) (Session, error) {
	var session Session
	err := r.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(user_id,1),BIN_TO_UUID(family_id,1),token_digest,created_at,last_used_at,expires_at,rotated_at,rotation_grace_ends_at,revoked_at FROM auth_sessions WHERE token_digest=?`, digest).Scan(
		&session.ID, &session.UserID, &session.FamilyID, &session.TokenDigest, &session.CreatedAt, &session.LastUsedAt, &session.ExpiresAt, &session.RotatedAt, &session.RotationGraceEnds, &session.RevokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidToken
	}
	if err != nil {
		return Session{}, fmt.Errorf("find session: %w", err)
	}
	return session, nil
}

func (r *MySQLRepository) RotateSession(ctx context.Context, currentSessionID string, replacement Session, rotatedAt, graceEnds time.Time) error {
	if !validUUID(currentSessionID) || !validUUID(replacement.ID) || !validUUID(replacement.UserID) || !validUUID(replacement.FamilyID) || len(replacement.TokenDigest) != 32 {
		return ErrConcurrentUpdate
	}
	return r.inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET rotated_at=?,rotation_grace_ends_at=? WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1) AND family_id=UUID_TO_BIN(?,1) AND rotated_at IS NULL AND revoked_at IS NULL`,
			rotatedAt.UTC(), graceEnds.UTC(), currentSessionID, replacement.UserID, replacement.FamilyID,
		)
		if err := expectOne(result, err, "rotate current session"); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO auth_sessions (id,user_id,family_id,token_digest,created_at,last_used_at,expires_at,rotated_at,rotation_grace_ends_at,revoked_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?,?,?)`,
			replacement.ID, replacement.UserID, replacement.FamilyID, replacement.TokenDigest, replacement.CreatedAt.UTC(), replacement.LastUsedAt.UTC(), replacement.ExpiresAt, replacement.RotatedAt, replacement.RotationGraceEnds, replacement.RevokedAt,
		)
		if err != nil {
			return fmt.Errorf("insert rotated session: %w", err)
		}
		return nil
	})
}

func (r *MySQLRepository) RevokeSessionByDigest(ctx context.Context, digest []byte, revokedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE token_digest=?`, revokedAt.UTC(), digest)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke session rows: %w", err)
	}
	if count != 1 {
		var exists int
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE token_digest=?)`, digest).Scan(&exists); err != nil {
			return fmt.Errorf("check revoked session: %w", err)
		}
		if exists != 1 {
			return ErrInvalidToken
		}
	}
	return nil
}

func (r *MySQLRepository) RevokeSessionByID(ctx context.Context, sessionID, userID string, revokedAt time.Time) error {
	if !validUUID(sessionID) || !validUUID(userID) {
		return ErrConcurrentUpdate
	}
	result, err := r.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1)`, revokedAt.UTC(), sessionID, userID)
	if err != nil {
		return fmt.Errorf("revoke session by id: %w", err)
	}
	// Revoking an already-revoked or unknown session is idempotent: the caller
	// still ends with that session revoked and there is no further state to
	// reconcile.
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke session by id rows: %w", err)
	}
	if count != 1 {
		var exists int
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1))`, sessionID, userID).Scan(&exists); err != nil {
			return fmt.Errorf("check revoked session by id: %w", err)
		}
		if exists == 0 {
			return ErrInvalidToken
		}
	}
	return nil
}

func (r *MySQLRepository) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	if !validUUID(userID) {
		return nil, ErrConcurrentUpdate
	}
	rows, err := r.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(user_id,1),BIN_TO_UUID(family_id,1),token_digest,created_at,last_used_at,expires_at,rotated_at,rotation_grace_ends_at,revoked_at FROM auth_sessions WHERE user_id=UUID_TO_BIN(?,1) ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.UserID, &session.FamilyID, &session.TokenDigest, &session.CreatedAt, &session.LastUsedAt, &session.ExpiresAt, &session.RotatedAt, &session.RotationGraceEnds, &session.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

func (r *MySQLRepository) RevokeSessionFamily(ctx context.Context, familyID string, revokedAt time.Time) error {
	if !validUUID(familyID) {
		return ErrConcurrentUpdate
	}
	_, err := r.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE family_id=UUID_TO_BIN(?,1)`, revokedAt.UTC(), familyID)
	if err != nil {
		return fmt.Errorf("revoke session family: %w", err)
	}
	return nil
}

func (r *MySQLRepository) RevokeAllSessions(ctx context.Context, userID string, revokedAt time.Time) error {
	if !validUUID(userID) {
		return ErrConcurrentUpdate
	}
	_, err := r.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE user_id=UUID_TO_BIN(?,1)`, revokedAt.UTC(), userID)
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

func (r *MySQLRepository) ChangePasswordAndRevokeSessions(ctx context.Context, userID, expectedHash, replacementHash string, changedAt time.Time) error {
	return r.changeCredentials(ctx, userID,
		`UPDATE users SET password_hash=? WHERE id=UUID_TO_BIN(?,1) AND password_hash=?`,
		[]any{replacementHash, userID, expectedHash}, changedAt, "change password")
}

func (r *MySQLRepository) ChangePasswordAndRevokeOtherSessions(ctx context.Context, userID, expectedHash, replacementHash string, keepSessionID string, changedAt time.Time) error {
	if !validUUID(userID) || !validUUID(keepSessionID) {
		return ErrConcurrentUpdate
	}
	return r.inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=UUID_TO_BIN(?,1) AND password_hash=?`, replacementHash, userID, expectedHash)
		if err := expectOne(result, err, "change password"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE user_id=UUID_TO_BIN(?,1) AND id<>UUID_TO_BIN(?,1)`, changedAt.UTC(), userID, keepSessionID); err != nil {
			return fmt.Errorf("change password revoke other sessions: %w", err)
		}
		return nil
	})
}

func (r *MySQLRepository) RecoverPassword(ctx context.Context, userID string, expectedRecoveryDigest []byte, replacementHash string, replacementRecoveryDigest []byte, changedAt time.Time) error {
	return r.changeCredentials(ctx, userID,
		`UPDATE users SET password_hash=?,recovery_code_digest=?,recovery_code_rotated_at=? WHERE id=UUID_TO_BIN(?,1) AND recovery_code_digest=?`,
		[]any{replacementHash, replacementRecoveryDigest, changedAt.UTC(), userID, expectedRecoveryDigest}, changedAt, "recover password")
}

func (r *MySQLRepository) RotateRecoveryCode(ctx context.Context, userID, expectedPasswordHash string, replacementRecoveryDigest []byte, changedAt time.Time) error {
	return r.changeCredentials(ctx, userID,
		`UPDATE users SET recovery_code_digest=?,recovery_code_rotated_at=? WHERE id=UUID_TO_BIN(?,1) AND password_hash=?`,
		[]any{replacementRecoveryDigest, changedAt.UTC(), userID, expectedPasswordHash}, changedAt, "rotate recovery code")
}

func (r *MySQLRepository) changeCredentials(ctx context.Context, userID, query string, args []any, changedAt time.Time, operation string) error {
	if !validUUID(userID) {
		return ErrConcurrentUpdate
	}
	return r.inTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, query, args...)
		if err := expectOne(result, err, operation); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE user_id=UUID_TO_BIN(?,1)`, changedAt.UTC(), userID); err != nil {
			return fmt.Errorf("%s revoke sessions: %w", operation, err)
		}
		return nil
	})
}

func (r *MySQLRepository) EmailExists(ctx context.Context, normalized string) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email_normalized=?)`, normalized).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check email: %w", err)
	}
	return exists == 1, nil
}

func (r *MySQLRepository) CreateRegisteredUser(ctx context.Context, input CreateRegisteredUserInput) error {
	if !validUUID(input.ID) || !validUUID(input.InitialSession.ID) || !validUUID(input.InitialSession.UserID) || !validUUID(input.InitialSession.FamilyID) || len(input.InitialSession.TokenDigest) != 32 {
		return fmt.Errorf("create registered user: invalid user or session data")
	}
	return r.inTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,password_hash,email_normalized,email_ciphertext,email_verified_at,recovery_code_digest,adult_confirmed_at,terms_version,status) VALUES (UUID_TO_BIN(?,1),?,?,?,?,?,?,?,?,?, 'active')`,
			input.ID, input.AccountName, input.Nickname, input.PasswordHash, input.EmailNormalized, input.EmailCiphertext, input.EmailVerifiedAt.UTC(), input.RecoveryCodeDigest, input.AdultConfirmedAt.UTC(), input.TermsVersion,
		); err != nil {
			if isDuplicateEntry(err) {
				if duplicateKeyName(err) == "uq_users_email_normalized" {
					return ErrEmailAlreadyRegistered
				}
				return ErrAccountNameTaken
			}
			return fmt.Errorf("insert registered user: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wallets (user_id,available_balance,reserved_balance,version) VALUES (UUID_TO_BIN(?,1),?,0,1)`, input.ID, input.InitialLight); err != nil {
			return fmt.Errorf("insert registered wallet: %w", err)
		}
		if input.InitialCoverAllowance > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_benefits (user_id,cover_allowance) VALUES (UUID_TO_BIN(?,1),?)`, input.ID, input.InitialCoverAllowance); err != nil {
				return fmt.Errorf("insert registered benefits: %w", err)
			}
		}
		if input.InitialLight > 0 {
			entryID, err := registrationRandomID()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'grant',?,0,?,0,'initial_light',NULL,?)`,
				entryID, input.ID, input.InitialLight, input.InitialLight, input.ID+":initial-light",
			); err != nil {
				return fmt.Errorf("insert initial light grant: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_sessions (id,user_id,family_id,token_digest,created_at,last_used_at,expires_at,rotated_at,rotation_grace_ends_at,revoked_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?,?,?)`,
			input.InitialSession.ID, input.InitialSession.UserID, input.InitialSession.FamilyID, input.InitialSession.TokenDigest, input.InitialSession.CreatedAt.UTC(), input.InitialSession.LastUsedAt.UTC(), input.InitialSession.ExpiresAt, input.InitialSession.RotatedAt, input.InitialSession.RotationGraceEnds, input.InitialSession.RevokedAt,
		); err != nil {
			return fmt.Errorf("insert initial session: %w", err)
		}
		return nil
	})
}

func (r *MySQLRepository) CountRegistrationsByRiskCookie(ctx context.Context, riskCookieDigest []byte, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM registration_risk_events WHERE risk_cookie_hash=? AND grant_granted=1 AND created_at>=?`, riskCookieDigest, since.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count risk cookie registrations: %w", err)
	}
	return count, nil
}

func (r *MySQLRepository) CreateRegistrationRiskEvent(ctx context.Context, event RegistrationRiskEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO registration_risk_events (email_hash,ip_prefix_hash,risk_cookie_hash,asn,turnstile_result,risk_level,action,grant_granted,expires_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		event.EmailHash, event.IPPrefixHash, event.RiskCookieHash, event.ASN, event.TurnstileResult, event.RiskLevel, event.Action, event.GrantGranted, event.CreatedAt.AddDate(0, 0, 30).UTC(), event.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create registration risk event: %w", err)
	}
	return nil
}

func (r *MySQLRepository) CreateEmailDeliveryLog(ctx context.Context, log EmailDeliveryLog) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO email_delivery_logs (email_hash,purpose,provider_msg_id,accepted,result,created_at) VALUES (?,?,?,?,?,?)`,
		log.EmailHash, log.Purpose, log.ProviderMsgID, log.Accepted, log.Result, log.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create email delivery log: %w", err)
	}
	return nil
}

func (r *MySQLRepository) WalletBalance(ctx context.Context, userID string) (int64, error) {
	if !validUUID(userID) {
		return 0, ErrConcurrentUpdate
	}
	var balance int64
	err := r.db.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, userID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("wallet balance: %w", err)
	}
	return balance, nil
}

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func duplicateKeyName(err error) string {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		for _, part := range strings.Split(mysqlErr.Message, " ") {
			part = strings.Trim(part, "'\"")
			if index := strings.Index(part, ".uq_"); index >= 0 {
				return part[index+1:]
			}
			if strings.HasPrefix(part, "uq_") {
				return part
			}
		}
	}
	return ""
}

func (r *MySQLRepository) inTransaction(ctx context.Context, operation func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin auth transaction: %w", err)
	}
	defer tx.Rollback()
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth transaction: %w", err)
	}
	return nil
}

func expectOne(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows: %w", operation, err)
	}
	if count != 1 {
		return ErrConcurrentUpdate
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	_, err := hex.DecodeString(compact)
	return err == nil
}

func registrationRandomID() (string, error) {
	return randomID(rand.Reader)
}
