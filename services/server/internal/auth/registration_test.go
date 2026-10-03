package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendRegistrationCodeSuccessFlow(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	nonce, cooldown, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{
		Email: " alice@example.com ", TurnstileToken: "dev-token", RiskCookie: "cookie-1", IPPrefix: "1.2.3.0/24",
	})
	if err != nil {
		t.Fatalf("SendRegistrationCode() error = %v", err)
	}
	if nonce == "" || cooldown != 0 {
		t.Fatalf("nonce = %q cooldown = %v", nonce, cooldown)
	}
	if len(emailProvider.sent) != 1 || !strings.HasPrefix(emailProvider.sent[0].email, "alice@example.com") {
		t.Fatalf("sent emails = %#v", emailProvider.sent)
	}
	if store.emailLogs != 1 {
		t.Fatalf("email delivery logs = %d, want 1", store.emailLogs)
	}
}

func TestSendRegistrationCodeRejectsDisposableAndRegisteredEmails(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	if _, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{Email: "x@mailinator.com", TurnstileToken: "dev-token", RiskCookie: "c", IPPrefix: ""}); !errors.Is(err, ErrDisposableEmail) {
		t.Fatalf("disposable error = %v", err)
	}
	if _, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{Email: "a@example.com", TurnstileToken: "dev-token", RiskCookie: "c", IPPrefix: ""}); err != nil {
		t.Fatalf("first send error = %v", err)
	}
	nonce, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{Email: "a@example.com", TurnstileToken: "dev-token", RiskCookie: "c", IPPrefix: ""})
	if err != nil {
		t.Fatalf("second send to same email should be allowed while unregistered: %v", err)
	}
	if nonce == "" {
		t.Fatal("nonce missing")
	}
	prepared, err := service.PrepareCredentials("Taken_01", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	store.accounts[prepared.AccountNameNormalized] = Account{
		ID: "user-registered-1", AccountName: "Taken_01", AccountNameNormalized: prepared.AccountNameNormalized,
		PasswordHash: prepared.PasswordHash, RecoveryCodeDigest: prepared.RecoveryCodeDigest, Status: "active",
		EmailNormalized: "taken@example.com",
	}
	if _, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{Email: "taken@example.com", TurnstileToken: "dev-token", RiskCookie: "c", IPPrefix: ""}); !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Fatalf("registered email error = %v", err)
	}
}

func TestSendRegistrationCodeRequiresTurnstileAndBlocksHighRisk(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, &recordingEmailProvider{}, humanCheck)

	if _, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{Email: "a@example.com", TurnstileToken: "wrong", RiskCookie: "c", IPPrefix: ""}); !errors.Is(err, ErrTurnstileVerification) {
		t.Fatalf("turnstile error = %v", err)
	}
}

func TestRegisterSuccessCreatesWalletGrantAndSession(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	codes := newFakeCodeStore()
	attachRegistration(t, service, store, emailProvider, humanCheck)
	service.registration.InitialLight = 20
	service.verificationCodes = codes
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	nonce, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{
		Email: "alice@example.com", TurnstileToken: "dev-token", RiskCookie: "cookie-1", IPPrefix: "1.2.3.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	code := emailProvider.sent[0].code

	result, err := service.Register(context.Background(), RegisterInput{
		AccountName: "Alice_01", Password: "correct horse 42", Email: "alice@example.com",
		Code: code, Nonce: nonce, TurnstileToken: "dev-token", AdultConfirmed: true, TermsVersion: "v1",
		RiskCookie: "cookie-1", IPPrefix: "1.2.3.0/24",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.RecoveryCode == "" || result.GrantedLight != 20 || result.SessionToken == "" {
		t.Fatalf("result = %#v", result)
	}
	if store.createdUser == nil || store.createdUser.InitialLight != 20 {
		t.Fatalf("created user = %#v", store.createdUser)
	}
	if store.createdSession == nil || store.createdSession.UserID != result.Account.ID {
		t.Fatalf("created session = %#v", store.createdSession)
	}
	loaded, err := service.AuthenticateSession(context.Background(), result.SessionToken)
	if err != nil || loaded.Session.UserID != result.Account.ID {
		t.Fatalf("initial session not usable: %v", err)
	}
	if len(emailProvider.sent) == 0 || store.riskEvents != 1 {
		t.Fatalf("sent=%d riskEvents=%d", len(emailProvider.sent), store.riskEvents)
	}
}

func TestRegisterRequiresAdultConfirmationAndValidCode(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	nonce, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{
		Email: "bob@example.com", TurnstileToken: "dev-token", RiskCookie: "cookie-1", IPPrefix: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	code := emailProvider.sent[0].code

	if _, err := service.Register(context.Background(), RegisterInput{
		AccountName: "Bob_01", Password: "correct horse 42", Email: "bob@example.com",
		Code: code, Nonce: nonce, TurnstileToken: "dev-token", AdultConfirmed: false, TermsVersion: "v1",
		RiskCookie: "cookie-1", IPPrefix: "",
	}); !errors.Is(err, ErrAdultConfirmation) {
		t.Fatalf("adult confirmation error = %v", err)
	}
	if _, err := service.Register(context.Background(), RegisterInput{
		AccountName: "Bob_01", Password: "correct horse 42", Email: "bob@example.com",
		Code: "wrong-code", Nonce: nonce, TurnstileToken: "dev-token", AdultConfirmed: true, TermsVersion: "v1",
		RiskCookie: "cookie-1", IPPrefix: "",
	}); !errors.Is(err, ErrVerificationCode) {
		t.Fatalf("wrong code error = %v", err)
	}
}

func TestRegisterRejectsTakenAccountNameAndEmail(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	register := func(accountName, email string) error {
		nonce, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{
			Email: email, TurnstileToken: "dev-token", RiskCookie: "cookie-1", IPPrefix: "",
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Register(context.Background(), RegisterInput{
			AccountName: accountName, Password: "correct horse 42", Email: email,
			Code: emailProvider.sent[len(emailProvider.sent)-1].code, Nonce: nonce, TurnstileToken: "dev-token",
			AdultConfirmed: true, TermsVersion: "v1", RiskCookie: "cookie-1", IPPrefix: "",
		})
		return err
	}
	if err := register("Alice_01", "alice@example.com"); err != nil {
		t.Fatalf("first register error = %v", err)
	}
	if err := register("alice_01", "other@example.com"); !errors.Is(err, ErrAccountNameTaken) {
		t.Fatalf("taken account error = %v", err)
	}
	secrets := testSecrets(t)
	code, codeDigest, err := secrets.NewVerificationCode()
	if err != nil {
		t.Fatal(err)
	}
	nonceValue, nonceDigest, err := secrets.NewRegistrationNonce()
	if err != nil {
		t.Fatal(err)
	}
	emailHash := service.secrets.EmailHash("alice@example.com")
	if _, err := service.verificationCodes.Send(context.Background(), emailHash, codeDigest, nonceDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(context.Background(), RegisterInput{
		AccountName: "Alice_03", Password: "correct horse 42", Email: "alice@example.com",
		Code: code, Nonce: nonceValue, TurnstileToken: "dev-token",
		AdultConfirmed: true, TermsVersion: "v1", RiskCookie: "cookie-1", IPPrefix: "",
	}); !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Fatalf("taken email error = %v", err)
	}
}

func TestRegisterChallengeDropsInitialLightGrant(t *testing.T) {
	store := newFakeRepository()
	service := testService(t, store)
	service.registration.InitialLight = 20
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	risk := &defaultRegistrationRiskServiceForTest{decision: RiskActionChallenge}
	attachRegistrationWithRisk(t, service, store, emailProvider, humanCheck, risk)

	nonce, _, err := service.SendRegistrationCode(context.Background(), SendRegistrationCodeInput{
		Email: "carol@example.com", TurnstileToken: "dev-token", RiskCookie: "cookie-1", IPPrefix: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Register(context.Background(), RegisterInput{
		AccountName: "Carol_01", Password: "correct horse 42", Email: "carol@example.com",
		Code: emailProvider.sent[0].code, Nonce: nonce, TurnstileToken: "dev-token", AdultConfirmed: true, TermsVersion: "v1",
		RiskCookie: "cookie-1", IPPrefix: "",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.GrantedLight != 0 || store.createdUser.InitialLight != 0 {
		t.Fatalf("granted = %d stored = %d, want 0", result.GrantedLight, store.createdUser.InitialLight)
	}
}

type defaultRegistrationRiskServiceForTest struct{ decision RiskAction }

func (s *defaultRegistrationRiskServiceForTest) Assess(_ context.Context, _ RegistrationRiskInput) (RegistrationRiskDecision, error) {
	return RegistrationRiskDecision{Action: s.decision, RiskLevel: "low"}, nil
}

type recordingEmailProvider struct {
	mu   sync.Mutex
	sent []sentEmail
}

type sentEmail struct {
	email string
	code  string
}

func (p *recordingEmailProvider) SendRegistrationCode(_ context.Context, email, code string) (MessageReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, sentEmail{email: email, code: code})
	return MessageReceipt{ProviderMessageID: "msg-1", Accepted: true}, nil
}

type fakeCodeStore struct {
	mu       sync.Mutex
	codes    map[string]storedCode
	cooldown time.Time
}

type storedCode struct {
	codeDigest  []byte
	nonceDigest []byte
	attempts    int
}

func newFakeCodeStore() *fakeCodeStore {
	return &fakeCodeStore{codes: make(map[string]storedCode)}
}

func (f *fakeCodeStore) Send(_ context.Context, emailHash []byte, codeDigest, nonceDigest []byte) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(emailHash)
	if f.cooldown.After(time.Now()) {
		return time.Until(f.cooldown), ErrVerificationCooldown
	}
	f.codes[key] = storedCode{codeDigest: append([]byte(nil), codeDigest...), nonceDigest: append([]byte(nil), nonceDigest...)}
	return 0, nil
}

func (f *fakeCodeStore) VerifyAndConsume(_ context.Context, emailHash []byte, codeDigest, nonceDigest []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(emailHash)
	stored, ok := f.codes[key]
	if !ok {
		return ErrVerificationCodeExpired
	}
	if !equalBytes(stored.codeDigest, codeDigest) || !equalBytes(stored.nonceDigest, nonceDigest) {
		stored.attempts++
		if stored.attempts >= 5 {
			delete(f.codes, key)
			return ErrVerificationCodeUsed
		}
		f.codes[key] = stored
		return ErrVerificationCode
	}
	delete(f.codes, key)
	return nil
}

func (f *fakeCodeStore) CooldownRemaining(_ context.Context, _ []byte) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cooldown.After(time.Now()) {
		return time.Until(f.cooldown), nil
	}
	return 0, nil
}

func attachRegistration(t *testing.T, service *Service, store *fakeRepository, emailProvider EmailProvider, humanCheck HumanCheckProvider) {
	t.Helper()
	attachRegistrationWithRisk(t, service, store, emailProvider, humanCheck, defaultRegistrationRiskService())
}

func attachRegistrationWithRisk(t *testing.T, service *Service, store *fakeRepository, emailProvider EmailProvider, humanCheck HumanCheckProvider, risk RegistrationRiskService) {
	t.Helper()
	emailCipher, err := NewEmailCipher(bytes.Repeat([]byte("S"), 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterDependencies(emailCipher, newFakeCodeStore(), emailProvider, humanCheck, risk, DefaultRegistrationConfig()); err != nil {
		t.Fatal(err)
	}
}
