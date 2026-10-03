package auth

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNormalizeAccountName(t *testing.T) {
	t.Parallel()

	got, err := NormalizeAccountName("  Alice_01 ")
	if err != nil || got != "alice_01" {
		t.Fatalf("NormalizeAccountName() = %q, %v", got, err)
	}
	for _, invalid := range []string{"abc", "this_name_is_over_twenty", "名字", "has-dash", "white space"} {
		if _, err := NormalizeAccountName(invalid); !errors.Is(err, ErrInvalidAccountName) {
			t.Errorf("NormalizeAccountName(%q) error = %v", invalid, err)
		}
	}
}

func TestNormalizeEmailPreservesProviderSpecificAddressing(t *testing.T) {
	t.Parallel()

	got, err := NormalizeEmail(" Alice.Example+story@Example.COM ")
	if err != nil || got != "alice.example+story@example.com" {
		t.Fatalf("NormalizeEmail() = %q, %v", got, err)
	}
	if got == "aliceexample@example.com" {
		t.Fatal("normalization must not remove dots or +tags")
	}
	for _, invalid := range []string{"", "not-an-email", "Name <a@example.com>", "a@@example.com", "用户@example.com"} {
		if _, err := NormalizeEmail(invalid); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) error = %v", invalid, err)
		}
	}
}

func TestPasswordHasherVerifiesAndSignalsUpgrade(t *testing.T) {
	t.Parallel()

	oldParams := testArgonParams()
	oldParams.Iterations = 1
	oldHasher, err := newPasswordHasher(oldParams, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := oldHasher.Hash("correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	current, err := newPasswordHasher(testArgonParams(), bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	matches, upgrade, err := current.Verify("correct horse 42", encoded)
	if err != nil || !matches || !upgrade {
		t.Fatalf("Verify() = matches %v, upgrade %v, err %v", matches, upgrade, err)
	}
	matches, _, err = current.Verify("wrong password", encoded)
	if err != nil || matches {
		t.Fatalf("wrong password Verify() = matches %v, err %v", matches, err)
	}
}

func TestSecretManagerUsesOpaqueValuesAndSeparatedDigests(t *testing.T) {
	t.Parallel()

	manager := testSecrets(t)
	token, tokenDigest, err := manager.NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	code, recoveryDigest, err := manager.NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || code == "" || token == code {
		t.Fatal("generated secrets must be non-empty and distinct")
	}
	if bytes.Contains(tokenDigest, []byte(token)) || bytes.Contains(recoveryDigest, []byte(code)) {
		t.Fatal("persisted digest leaked plaintext")
	}
	if equalDigest(manager.SessionDigest(code), manager.RecoveryCodeDigest(code)) {
		t.Fatal("session and recovery digest domains must be separated")
	}
}

func TestSessionRotationAndReplayRevokesFamily(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.config.SessionRotationInterval = time.Hour
	service.config.SessionRotationGrace = time.Minute

	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	login, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(store.sessions[login.Session.ID].TokenDigest, []byte(login.SessionToken)) {
		t.Fatal("repository stored session plaintext")
	}

	now = now.Add(2 * time.Hour)
	rotated, err := service.AuthenticateSession(context.Background(), login.SessionToken)
	if err != nil || rotated.ReplacementToken == "" || rotated.Session.FamilyID != login.Session.FamilyID {
		t.Fatalf("rotation result = %#v, err %v", rotated, err)
	}

	now = now.Add(30 * time.Second)
	if _, err := service.AuthenticateSession(context.Background(), login.SessionToken); err != nil {
		t.Fatalf("old token should work inside grace: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := service.AuthenticateSession(context.Background(), login.SessionToken); !errors.Is(err, ErrSessionReplay) {
		t.Fatalf("old token after grace error = %v", err)
	}
	if _, err := service.AuthenticateSession(context.Background(), rotated.ReplacementToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("replacement after replay error = %v", err)
	}
}

func TestChangePasswordRevokesAllSessions(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	first, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.ChangePassword(context.Background(), account, "correct horse 42", "replacement pass 7"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{first.SessionToken, second.SessionToken} {
		if _, err := service.AuthenticateSession(context.Background(), token); !errors.Is(err, ErrSessionRevoked) {
			t.Fatalf("session after password change error = %v", err)
		}
	}
	if _, err := service.Login(context.Background(), account.AccountName, "correct horse 42"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login error = %v", err)
	}
	if _, err := service.Login(context.Background(), account.AccountName, "replacement pass 7"); err != nil {
		t.Fatalf("replacement password login: %v", err)
	}
}

func TestRecoveryCodeIsOneTimeRotatedAndRevokesAllSessions(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	prepared, err := service.PrepareCredentials("Alice_01", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	account := Account{ID: "user-1", AccountName: "Alice_01", AccountNameNormalized: prepared.AccountNameNormalized, PasswordHash: prepared.PasswordHash, RecoveryCodeDigest: prepared.RecoveryCodeDigest, Status: "active"}
	store.accounts[account.AccountNameNormalized] = account
	login, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	replacementCode, err := service.RecoverPassword(context.Background(), account.AccountName, prepared.RecoveryCode, "replacement pass 7")
	if err != nil || replacementCode == "" || replacementCode == prepared.RecoveryCode {
		t.Fatalf("RecoverPassword() code = %q, err %v", replacementCode, err)
	}
	if _, err := service.AuthenticateSession(context.Background(), login.SessionToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session after recovery error = %v", err)
	}
	if _, err := service.RecoverPassword(context.Background(), account.AccountName, prepared.RecoveryCode, "another password 9"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("reused recovery code error = %v", err)
	}
	if _, err := service.RecoverPassword(context.Background(), account.AccountName, replacementCode, "another password 9"); err != nil {
		t.Fatalf("rotated recovery code: %v", err)
	}
}

func testArgonParams() Argon2idParams {
	return Argon2idParams{MemoryKiB: 8 * 1024, Iterations: 2, Parallelism: 1, SaltBytes: 16, KeyBytes: 16}
}

func testSecrets(t *testing.T) *SecretManager {
	t.Helper()
	// Fixed synthetic bytes are test fixtures, not deployable secrets.
	manager, err := newSecretManager(bytes.Repeat([]byte("S"), 32), bytes.Repeat([]byte("P"), 32), &countingReader{next: 3})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testService(t *testing.T, store *fakeRepository) *Service {
	t.Helper()
	hasher, err := newPasswordHasher(testArgonParams(), bytes.NewReader(bytes.Repeat([]byte{4}, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, hasher, testSecrets(t), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	service.random = &countingReader{next: 5}
	return service
}

func addTestAccount(t *testing.T, service *Service, store *fakeRepository, name, password string) Account {
	t.Helper()
	prepared, err := service.PrepareCredentials(name, password)
	if err != nil {
		t.Fatal(err)
	}
	account := Account{ID: "user-1", AccountName: name, AccountNameNormalized: prepared.AccountNameNormalized, PasswordHash: prepared.PasswordHash, RecoveryCodeDigest: prepared.RecoveryCodeDigest, Status: "active"}
	store.accounts[prepared.AccountNameNormalized] = account
	return account
}

type fakeRepository struct {
	mu             sync.Mutex
	accounts       map[string]Account
	sessions       map[string]Session
	balances       map[string]int64
	emailLogs      int
	riskEvents     int
	createdUser    *CreateRegisteredUserInput
	createdSession *Session
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{accounts: make(map[string]Account), sessions: make(map[string]Session), balances: make(map[string]int64)}
}

func (r *fakeRepository) FindAccountByNormalizedName(_ context.Context, normalized string) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[normalized]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	return cloneAccount(account), nil
}

func (r *fakeRepository) EmailExists(_ context.Context, normalized string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.EmailNormalized == normalized {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) CreateRegisteredUser(_ context.Context, input CreateRegisteredUserInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.accounts[input.AccountNameNormalized]; ok {
		return ErrAccountNameTaken
	}
	for _, account := range r.accounts {
		if account.EmailNormalized == input.EmailNormalized {
			return ErrEmailAlreadyRegistered
		}
	}
	user := &CreateRegisteredUserInput{
		ID:                    input.ID,
		AccountName:           input.AccountName,
		AccountNameNormalized: input.AccountNameNormalized,
		Nickname:              input.Nickname,
		PasswordHash:          input.PasswordHash,
		EmailNormalized:       input.EmailNormalized,
		EmailCiphertext:       append([]byte(nil), input.EmailCiphertext...),
		EmailVerifiedAt:       input.EmailVerifiedAt,
		AdultConfirmedAt:      input.AdultConfirmedAt,
		TermsVersion:          input.TermsVersion,
		RecoveryCodeDigest:    append([]byte(nil), input.RecoveryCodeDigest...),
		InitialLight:          input.InitialLight,
		InitialSession:        cloneSession(input.InitialSession),
	}
	r.createdUser = user
	r.accounts[input.AccountNameNormalized] = Account{
		ID:                    input.ID,
		AccountName:           input.AccountName,
		AccountNameNormalized: input.AccountNameNormalized,
		PasswordHash:          input.PasswordHash,
		RecoveryCodeDigest:    input.RecoveryCodeDigest,
		Status:                "active",
		EmailNormalized:       input.EmailNormalized,
	}
	r.sessions[input.InitialSession.ID] = cloneSession(input.InitialSession)
	session := cloneSession(input.InitialSession)
	r.createdSession = &session
	return nil
}

func (r *fakeRepository) CountRegistrationsByRiskCookie(_ context.Context, _ []byte, _ time.Time) (int, error) {
	return 0, nil
}

func (r *fakeRepository) CreateRegistrationRiskEvent(_ context.Context, _ RegistrationRiskEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.riskEvents++
	return nil
}

func (r *fakeRepository) CreateEmailDeliveryLog(_ context.Context, _ EmailDeliveryLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emailLogs++
	return nil
}

func (r *fakeRepository) FindAccountByID(_ context.Context, userID string) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.ID == userID {
			return cloneAccount(account), nil
		}
	}
	return Account{}, ErrAccountNotFound
}

func (r *fakeRepository) WalletBalance(_ context.Context, userID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.balances[userID], nil
}

func (r *fakeRepository) RevokeSessionByID(_ context.Context, sessionID, userID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.ID == sessionID && session.UserID == userID {
			session.RevokedAt = timePointer(revokedAt)
			r.sessions[id] = session
			return nil
		}
	}
	return ErrInvalidToken
}

func (r *fakeRepository) ListSessions(_ context.Context, userID string) ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sessions []Session
	for _, session := range r.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			sessions = append(sessions, cloneSession(session))
		}
	}
	return sessions, nil
}

func (r *fakeRepository) ChangePasswordAndRevokeOtherSessions(_ context.Context, userID, expectedHash, replacementHash string, keepSessionID string, changedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, account, ok := r.accountByID(userID)
	if !ok || account.PasswordHash != expectedHash {
		return ErrConcurrentUpdate
	}
	account.PasswordHash = replacementHash
	r.accounts[key] = account
	for id, session := range r.sessions {
		if session.UserID == userID && session.ID != keepSessionID {
			session.RevokedAt = timePointer(changedAt)
			r.sessions[id] = session
		}
	}
	return nil
}

func (r *fakeRepository) UpdatePasswordHash(_ context.Context, userID, expectedHash, replacementHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, account, ok := r.accountByID(userID)
	if !ok || account.PasswordHash != expectedHash {
		return ErrConcurrentUpdate
	}
	account.PasswordHash = replacementHash
	r.accounts[key] = account
	return nil
}

func (r *fakeRepository) CreateSession(_ context.Context, session Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = cloneSession(session)
	return nil
}

func (r *fakeRepository) FindSessionByDigest(_ context.Context, digest []byte) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, session := range r.sessions {
		if equalDigest(session.TokenDigest, digest) {
			return cloneSession(session), nil
		}
	}
	return Session{}, ErrInvalidToken
}

func (r *fakeRepository) RotateSession(_ context.Context, currentSessionID string, replacement Session, rotatedAt, graceEnds time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[currentSessionID]
	if !ok || current.RotatedAt != nil || current.RevokedAt != nil {
		return ErrConcurrentUpdate
	}
	current.RotatedAt = timePointer(rotatedAt)
	current.RotationGraceEnds = timePointer(graceEnds)
	r.sessions[currentSessionID] = current
	r.sessions[replacement.ID] = cloneSession(replacement)
	return nil
}

func (r *fakeRepository) RevokeSessionByDigest(_ context.Context, digest []byte, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if equalDigest(session.TokenDigest, digest) {
			session.RevokedAt = timePointer(revokedAt)
			r.sessions[id] = session
			return nil
		}
	}
	return ErrInvalidToken
}

func (r *fakeRepository) RevokeSessionFamily(_ context.Context, familyID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.FamilyID == familyID {
			session.RevokedAt = timePointer(revokedAt)
			r.sessions[id] = session
		}
	}
	return nil
}

func (r *fakeRepository) RevokeAllSessions(_ context.Context, userID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revokeAll(userID, revokedAt)
	return nil
}

func (r *fakeRepository) ChangePasswordAndRevokeSessions(_ context.Context, userID, expectedHash, replacementHash string, changedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, account, ok := r.accountByID(userID)
	if !ok || account.PasswordHash != expectedHash {
		return ErrConcurrentUpdate
	}
	account.PasswordHash = replacementHash
	r.accounts[key] = account
	r.revokeAll(userID, changedAt)
	return nil
}

func (r *fakeRepository) RecoverPassword(_ context.Context, userID string, expectedRecoveryDigest []byte, replacementHash string, replacementRecoveryDigest []byte, changedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, account, ok := r.accountByID(userID)
	if !ok || !equalDigest(account.RecoveryCodeDigest, expectedRecoveryDigest) {
		return ErrConcurrentUpdate
	}
	account.PasswordHash = replacementHash
	account.RecoveryCodeDigest = append([]byte(nil), replacementRecoveryDigest...)
	r.accounts[key] = account
	r.revokeAll(userID, changedAt)
	return nil
}

func (r *fakeRepository) RotateRecoveryCode(_ context.Context, userID, expectedPasswordHash string, replacementRecoveryDigest []byte, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, account, ok := r.accountByID(userID)
	if !ok || account.PasswordHash != expectedPasswordHash {
		return ErrConcurrentUpdate
	}
	account.RecoveryCodeDigest = append([]byte(nil), replacementRecoveryDigest...)
	r.accounts[key] = account
	return nil
}

func (r *fakeRepository) accountByID(userID string) (string, Account, bool) {
	for key, account := range r.accounts {
		if account.ID == userID {
			return key, account, true
		}
	}
	return "", Account{}, false
}

func (r *fakeRepository) revokeAll(userID string, revokedAt time.Time) {
	for id, session := range r.sessions {
		if session.UserID == userID {
			session.RevokedAt = timePointer(revokedAt)
			r.sessions[id] = session
		}
	}
}

func cloneAccount(account Account) Account {
	account.RecoveryCodeDigest = append([]byte(nil), account.RecoveryCodeDigest...)
	return account
}

func (r *fakeRepository) FindAccountByEmail(_ context.Context, normalized string) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.EmailNormalized == normalized {
			return cloneAccount(account), nil
		}
	}
	return Account{}, ErrAccountNotFound
}

func cloneSession(session Session) Session {
	session.TokenDigest = append([]byte(nil), session.TokenDigest...)
	return session
}

func timePointer(value time.Time) *time.Time { return &value }

type countingReader struct{ next byte }

func (r *countingReader) Read(target []byte) (int, error) {
	for index := range target {
		target[index] = r.next
		r.next++
	}
	return len(target), nil
}
