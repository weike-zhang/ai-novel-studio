package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

type Session struct {
	ID                string
	UserID            string
	FamilyID          string
	TokenDigest       []byte
	CreatedAt         time.Time
	LastUsedAt        time.Time
	ExpiresAt         *time.Time
	RotatedAt         *time.Time
	RotationGraceEnds *time.Time
	RevokedAt         *time.Time
}

type Repository interface {
	FindAccountByNormalizedName(ctx context.Context, normalized string) (Account, error)
	FindAccountByID(ctx context.Context, userID string) (Account, error)
	UpdatePasswordHash(ctx context.Context, userID, expectedHash, replacementHash string) error

	EmailExists(ctx context.Context, normalized string) (bool, error)
	CreateRegisteredUser(ctx context.Context, input CreateRegisteredUserInput) error
	CountRegistrationsByRiskCookie(ctx context.Context, riskCookieDigest []byte, since time.Time) (int, error)
	CreateRegistrationRiskEvent(ctx context.Context, event RegistrationRiskEvent) error
	CreateEmailDeliveryLog(ctx context.Context, log EmailDeliveryLog) error
	WalletBalance(ctx context.Context, userID string) (int64, error)

	CreateSession(ctx context.Context, session Session) error
	FindSessionByDigest(ctx context.Context, digest []byte) (Session, error)
	RotateSession(ctx context.Context, currentSessionID string, replacement Session, rotatedAt, graceEnds time.Time) error
	RevokeSessionByDigest(ctx context.Context, digest []byte, revokedAt time.Time) error
	RevokeSessionByID(ctx context.Context, sessionID, userID string, revokedAt time.Time) error
	RevokeSessionFamily(ctx context.Context, familyID string, revokedAt time.Time) error
	RevokeAllSessions(ctx context.Context, userID string, revokedAt time.Time) error
	ListSessions(ctx context.Context, userID string) ([]Session, error)

	// These compound methods must update credentials and revoke every session
	// in one database transaction.
	ChangePasswordAndRevokeSessions(ctx context.Context, userID, expectedHash, replacementHash string, changedAt time.Time) error
	ChangePasswordAndRevokeOtherSessions(ctx context.Context, userID, expectedHash, replacementHash string, keepSessionID string, changedAt time.Time) error
	RecoverPassword(ctx context.Context, userID string, expectedRecoveryDigest []byte, replacementHash string, replacementRecoveryDigest []byte, changedAt time.Time) error
	RotateRecoveryCode(ctx context.Context, userID, expectedPasswordHash string, replacementRecoveryDigest []byte, changedAt time.Time) error
}

type Config struct {
	SessionRotationInterval time.Duration
	SessionRotationGrace    time.Duration
}

func DefaultConfig() Config {
	return Config{SessionRotationInterval: 30 * 24 * time.Hour, SessionRotationGrace: 30 * time.Second}
}

type Service struct {
	repository        Repository
	passwords         *PasswordHasher
	secrets           *SecretManager
	config            Config
	registration      RegistrationConfig
	emailCipher       *EmailCipher
	verificationCodes VerificationCodeStore
	emailProvider     EmailProvider
	humanCheck        HumanCheckProvider
	risk              RegistrationRiskService
	now               func() time.Time
	random            io.Reader
}

func NewService(repository Repository, passwords *PasswordHasher, secrets *SecretManager, config Config) (*Service, error) {
	if repository == nil || passwords == nil || secrets == nil || config.SessionRotationInterval <= 0 || config.SessionRotationGrace < 0 {
		return nil, ErrInvalidConfiguration
	}
	return &Service{repository: repository, passwords: passwords, secrets: secrets, config: config, registration: DefaultRegistrationConfig(), now: time.Now, random: rand.Reader}, nil
}

// RegisterDependencies wires the registration flow into the service. It is a
// separate setter so account/session tests keep a minimal service.
func (s *Service) RegisterDependencies(emailCipher *EmailCipher, codes VerificationCodeStore, emailProvider EmailProvider, humanCheck HumanCheckProvider, risk RegistrationRiskService, registration RegistrationConfig) error {
	if emailCipher == nil || codes == nil || emailProvider == nil || humanCheck == nil || risk == nil {
		return ErrInvalidConfiguration
	}
	s.emailCipher = emailCipher
	s.verificationCodes = codes
	s.emailProvider = emailProvider
	s.humanCheck = humanCheck
	s.risk = risk
	if registration.TermsVersion != "" {
		if registration.InitialLight > 0 {
			s.registration.InitialLight = registration.InitialLight
		}
		if registration.InitialCoverAllowance > 0 {
			s.registration.InitialCoverAllowance = registration.InitialCoverAllowance
		}
		if registration.RiskCookieTTL > 0 {
			s.registration.RiskCookieTTL = registration.RiskCookieTTL
		}
		s.registration.MaxAccountsPerIP = registration.MaxAccountsPerIP
		s.registration.TermsVersion = registration.TermsVersion
	}
	return nil
}

type PreparedCredentials struct {
	AccountNameNormalized string
	PasswordHash          string
	RecoveryCode          string
	RecoveryCodeDigest    []byte
}

// PrepareCredentials returns a recovery code exactly once to the caller. Only
// RecoveryCodeDigest is intended for persistence.
func (s *Service) PrepareCredentials(accountName, password string) (PreparedCredentials, error) {
	normalized, err := NormalizeAccountName(accountName)
	if err != nil {
		return PreparedCredentials{}, err
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return PreparedCredentials{}, err
	}
	code, digest, err := s.secrets.NewRecoveryCode()
	if err != nil {
		return PreparedCredentials{}, err
	}
	return PreparedCredentials{AccountNameNormalized: normalized, PasswordHash: hash, RecoveryCode: code, RecoveryCodeDigest: digest}, nil
}

type LoginResult struct {
	Account      Account
	Session      Session
	SessionToken string
}

func (s *Service) Login(ctx context.Context, accountName, password string) (LoginResult, error) {
	normalized, err := NormalizeAccountName(accountName)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	account, err := s.repository.FindAccountByNormalizedName(ctx, normalized)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	matches, upgrade, err := s.passwords.Verify(password, account.PasswordHash)
	if err != nil || !matches {
		return LoginResult{}, ErrInvalidCredentials
	}
	if upgrade {
		replacement, hashErr := s.passwords.Hash(password)
		if hashErr != nil {
			return LoginResult{}, hashErr
		}
		if err := s.repository.UpdatePasswordHash(ctx, account.ID, account.PasswordHash, replacement); err != nil && !errors.Is(err, ErrConcurrentUpdate) {
			return LoginResult{}, err
		}
		account.PasswordHash = replacement
	}
	session, token, err := s.newSession(account.ID, "")
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Account: account, Session: session, SessionToken: token}, nil
}

type SessionResult struct {
	Session          Session
	ReplacementToken string
}

func (s *Service) AuthenticateSession(ctx context.Context, token string) (SessionResult, error) {
	if token == "" {
		return SessionResult{}, ErrInvalidToken
	}
	digest := s.secrets.SessionDigest(token)
	session, err := s.repository.FindSessionByDigest(ctx, digest)
	if err != nil || !equalDigest(session.TokenDigest, digest) {
		return SessionResult{}, ErrInvalidToken
	}
	now := s.now().UTC()
	if session.RevokedAt != nil || (session.ExpiresAt != nil && !now.Before(*session.ExpiresAt)) {
		return SessionResult{}, ErrSessionRevoked
	}
	if session.RotatedAt != nil {
		if session.RotationGraceEnds != nil && !now.After(*session.RotationGraceEnds) {
			return SessionResult{Session: session}, nil
		}
		if err := s.repository.RevokeSessionFamily(ctx, session.FamilyID, now); err != nil {
			return SessionResult{}, err
		}
		return SessionResult{}, ErrSessionReplay
	}
	if now.Sub(session.CreatedAt) < s.config.SessionRotationInterval {
		return SessionResult{Session: session}, nil
	}
	replacement, plaintext, err := s.newSession(session.UserID, session.FamilyID)
	if err != nil {
		return SessionResult{}, err
	}
	graceEnds := now.Add(s.config.SessionRotationGrace)
	if err := s.repository.RotateSession(ctx, session.ID, replacement, now, graceEnds); err != nil {
		return SessionResult{}, err
	}
	return SessionResult{Session: replacement, ReplacementToken: plaintext}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidToken
	}
	return s.repository.RevokeSessionByDigest(ctx, s.secrets.SessionDigest(token), s.now().UTC())
}

// LogoutFamily revokes both the current token and any short-lived rotation
// predecessor/successor in the same logical browser session.
func (s *Service) LogoutFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return ErrInvalidToken
	}
	return s.repository.RevokeSessionFamily(ctx, familyID, s.now().UTC())
}

func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	return s.repository.RevokeAllSessions(ctx, userID, s.now().UTC())
}

func (s *Service) ChangePassword(ctx context.Context, account Account, currentPassword, replacementPassword string) error {
	matches, _, err := s.passwords.Verify(currentPassword, account.PasswordHash)
	if err != nil || !matches {
		return ErrInvalidCredentials
	}
	replacement, err := s.passwords.Hash(replacementPassword)
	if err != nil {
		return err
	}
	return s.repository.ChangePasswordAndRevokeSessions(ctx, account.ID, account.PasswordHash, replacement, s.now().UTC())
}

// ChangePasswordKeepSession updates the password and revokes every other
// session while keeping the current one valid, matching the account page
// behaviour. The current session is validated by the caller before this runs.
func (s *Service) ChangePasswordKeepSession(ctx context.Context, account Account, currentPassword, replacementPassword, keepSessionID string) error {
	matches, _, err := s.passwords.Verify(currentPassword, account.PasswordHash)
	if err != nil || !matches {
		return ErrInvalidCredentials
	}
	replacement, err := s.passwords.Hash(replacementPassword)
	if err != nil {
		return err
	}
	return s.repository.ChangePasswordAndRevokeOtherSessions(ctx, account.ID, account.PasswordHash, replacement, keepSessionID, s.now().UTC())
}

func (s *Service) RecoverPassword(ctx context.Context, accountName, recoveryCode, replacementPassword string) (string, error) {
	normalized, err := NormalizeAccountName(accountName)
	if err != nil {
		return "", ErrInvalidRecoveryCode
	}
	account, err := s.repository.FindAccountByNormalizedName(ctx, normalized)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return "", ErrInvalidRecoveryCode
		}
		return "", err
	}
	presented := s.secrets.RecoveryCodeDigest(recoveryCode)
	if !equalDigest(presented, account.RecoveryCodeDigest) {
		return "", ErrInvalidRecoveryCode
	}
	replacementHash, err := s.passwords.Hash(replacementPassword)
	if err != nil {
		return "", err
	}
	newCode, newDigest, err := s.secrets.NewRecoveryCode()
	if err != nil {
		return "", err
	}
	if err := s.repository.RecoverPassword(ctx, account.ID, account.RecoveryCodeDigest, replacementHash, newDigest, s.now().UTC()); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return "", ErrInvalidRecoveryCode
		}
		return "", err
	}
	return newCode, nil
}

func (s *Service) RotateRecoveryCode(ctx context.Context, account Account, currentPassword string) (string, error) {
	matches, _, err := s.passwords.Verify(currentPassword, account.PasswordHash)
	if err != nil || !matches {
		return "", ErrInvalidCredentials
	}
	code, digest, err := s.secrets.NewRecoveryCode()
	if err != nil {
		return "", err
	}
	if err := s.repository.RotateRecoveryCode(ctx, account.ID, account.PasswordHash, digest, s.now().UTC()); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Service) newSession(userID, familyID string) (Session, string, error) {
	token, digest, err := s.secrets.NewSessionToken()
	if err != nil {
		return Session{}, "", err
	}
	id, err := randomID(s.random)
	if err != nil {
		return Session{}, "", err
	}
	if familyID == "" {
		familyID, err = randomID(s.random)
		if err != nil {
			return Session{}, "", err
		}
	}
	now := s.now().UTC()
	return Session{ID: id, UserID: userID, FamilyID: familyID, TokenDigest: digest, CreatedAt: now, LastUsedAt: now}, token, nil
}

func randomID(random io.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
