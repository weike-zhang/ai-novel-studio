package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// RegistrationConfig controls the registration flow. Sensitive values such as
// the risk-cookie TTL are set here; the code store and providers carry their
// own transport configuration.
type RegistrationConfig struct {
	InitialLight          int64
	InitialCoverAllowance int
	RiskCookieTTL         time.Duration
	MaxAccountsPerIP      int
	TermsVersion          string
}

func DefaultRegistrationConfig() RegistrationConfig {
	return RegistrationConfig{
		InitialLight:          100,
		InitialCoverAllowance: 3,
		RiskCookieTTL:         30 * 24 * time.Hour,
		MaxAccountsPerIP:      5,
		TermsVersion:          "v1",
	}
}

// CreateRegisteredUserInput is everything the single registration transaction
// needs: the account row, the wallet with an optional initial-light grant, the
// recovery digest and the first session.
type CreateRegisteredUserInput struct {
	ID                    string
	AccountName           string
	AccountNameNormalized string
	Nickname              string
	PasswordHash          string
	EmailNormalized       string
	EmailCiphertext       []byte
	EmailVerifiedAt       time.Time
	AdultConfirmedAt      time.Time
	TermsVersion          string
	RecoveryCodeDigest    []byte
	InitialLight          int64
	InitialCoverAllowance int
	InitialSession        Session
}

type RegistrationRiskEvent struct {
	EmailHash      []byte
	IPPrefixHash   []byte
	RiskCookieHash []byte
	ASN            string
	TurnstileResult string
	RiskLevel      string
	Action         string
	GrantGranted   bool
	CreatedAt      time.Time
}

type EmailDeliveryLog struct {
	EmailHash      []byte
	Purpose        string
	ProviderMsgID  string
	Accepted       bool
	Result         string
	CreatedAt      time.Time
}

type RegistrationResult struct {
	Account      Account
	Session      Session
	SessionToken string
	RecoveryCode string
	GrantedLight int64
}

func (s *Service) SendRegistrationCode(ctx context.Context, input SendRegistrationCodeInput) (nonce string, cooldown time.Duration, err error) {
	nonce, cooldown, _, err = s.sendRegistrationCode(ctx, input)
	return nonce, cooldown, err
}

// SendRegistrationCodeWithEcho behaves like SendRegistrationCode but also
// returns the generated code. It must only be used in development where the
// synthetic email provider cannot deliver real mail. Production handlers must
// use SendRegistrationCode and never expose the code.
func (s *Service) SendRegistrationCodeWithEcho(ctx context.Context, input SendRegistrationCodeInput) (nonce string, cooldown time.Duration, code string, err error) {
	return s.sendRegistrationCode(ctx, input)
}

func (s *Service) sendRegistrationCode(ctx context.Context, input SendRegistrationCodeInput) (nonce string, cooldown time.Duration, code string, err error) {
	normalized, err := NormalizeEmail(input.Email)
	if err != nil {
		return "", 0, "", err
	}
	if isDisposableEmail(normalized) {
		return "", 0, "", ErrDisposableEmail
	}
	exists, err := s.repository.EmailExists(ctx, normalized)
	if err != nil {
		return "", 0, "", err
	}
	if exists {
		return "", 0, "", ErrEmailAlreadyRegistered
	}
	check, err := s.humanCheck.VerifyRegistration(ctx, input.TurnstileToken, input.RemoteIP)
	if err != nil {
		return "", 0, "", err
	}
	if !check.Success {
		return "", 0, "", ErrTurnstileVerification
	}
	emailHash := s.secrets.EmailHash(normalized)
	riskCookieDigest := s.secrets.RiskCookieDigest(input.RiskCookie)
	decision, err := s.risk.Assess(ctx, RegistrationRiskInput{
		RiskCookieDigest: riskCookieDigest,
		IPPrefix:         input.IPPrefix,
		ASN:              "",
		EmailHash:        emailHash,
		EmailNormalized:  normalized,
		Disposable:       isDisposableEmail(normalized),
		Turnstile:        check,
		Now:              s.now().UTC(),
	})
	if err != nil {
		return "", 0, "", err
	}
	if decision.Action == RiskActionBlock || decision.Action == RiskActionManualReview {
		return "", 0, "", ErrRegistrationBlocked
	}
	codeValue, codeDigest, err := s.secrets.NewVerificationCode()
	if err != nil {
		return "", 0, "", err
	}
	nonceValue, nonceDigest, err := s.secrets.NewRegistrationNonce()
	if err != nil {
		return "", 0, "", err
	}
	cooldown, err = s.verificationCodes.Send(ctx, emailHash, codeDigest, nonceDigest)
	if err != nil {
		return "", 0, "", err
	}
	receipt, err := s.emailProvider.SendRegistrationCode(ctx, normalized, codeValue)
	delivery := EmailDeliveryLog{
		EmailHash: emailHash,
		Purpose:   "registration",
		Accepted:  receipt.Accepted,
		Result:    receipt.Result,
		CreatedAt: s.now().UTC(),
	}
	if receipt.ProviderMessageID != "" {
		delivery.ProviderMsgID = receipt.ProviderMessageID
	}
	if err != nil {
		_ = s.repository.CreateEmailDeliveryLog(ctx, delivery)
		return "", 0, "", err
	}
	if err := s.repository.CreateEmailDeliveryLog(ctx, delivery); err != nil {
		return "", 0, "", err
	}
	return nonceValue, cooldown, codeValue, nil
}

type SendRegistrationCodeInput struct {
	Email          string
	TurnstileToken string
	RemoteIP       string
	RiskCookie     string
	IPPrefix       string
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (RegistrationResult, error) {
	now := s.now().UTC()
	if !input.AdultConfirmed || input.TermsVersion == "" {
		return RegistrationResult{}, ErrAdultConfirmation
	}
	if err := ValidatePassword(input.Password); err != nil {
		return RegistrationResult{}, err
	}
	normalizedEmail, err := NormalizeEmail(input.Email)
	if err != nil {
		return RegistrationResult{}, err
	}
	check, err := s.humanCheck.VerifyRegistration(ctx, input.TurnstileToken, input.RemoteIP)
	if err != nil {
		return RegistrationResult{}, err
	}
	if !check.Success {
		return RegistrationResult{}, ErrTurnstileVerification
	}
	emailHash := s.secrets.EmailHash(normalizedEmail)
	riskCookieDigest := s.secrets.RiskCookieDigest(input.RiskCookie)
	accountsPerCookie, err := s.repository.CountRegistrationsByRiskCookie(ctx, riskCookieDigest, now.AddDate(0, 0, -30))
	if err != nil {
		return RegistrationResult{}, err
	}
	decision, err := s.risk.Assess(ctx, RegistrationRiskInput{
		RiskCookieDigest:  riskCookieDigest,
		IPPrefix:          input.IPPrefix,
		ASN:               "",
		EmailHash:         emailHash,
		EmailNormalized:   normalizedEmail,
		Disposable:        isDisposableEmail(normalizedEmail),
		Turnstile:         check,
		AccountsPerCookie: accountsPerCookie,
		Now:               now,
	})
	if err != nil {
		return RegistrationResult{}, err
	}
	switch decision.Action {
	case RiskActionBlock, RiskActionManualReview:
		return RegistrationResult{}, ErrRegistrationBlocked
	case RiskActionChallenge:
		decision = RegistrationRiskDecision{Action: RiskActionAllowWithoutGrant, RiskLevel: "medium", Reason: "challenged"}
	}
	nonceDigest := s.secrets.RegistrationNonceDigest(input.Nonce)
	codeDigest := s.secrets.VerificationCodeDigest(input.Code)
	if err := s.verificationCodes.VerifyAndConsume(ctx, emailHash, codeDigest, nonceDigest); err != nil {
		return RegistrationResult{}, err
	}
	prepared, err := s.PrepareCredentials(input.AccountName, input.Password)
	if err != nil {
		return RegistrationResult{}, err
	}
	userID, err := randomID(s.random)
	if err != nil {
		return RegistrationResult{}, err
	}
	emailCiphertext, err := s.emailCipher.SealEmail(randomNonce(emailCipherNonceSize), input.Email)
	if err != nil {
		return RegistrationResult{}, err
	}
	session, token, err := s.newSession(userID, "")
	if err != nil {
		return RegistrationResult{}, err
	}
	granted := int64(0)
	if decision.Action == RiskActionAllow {
		granted = s.registration.InitialLight
	}
	createErr := s.repository.CreateRegisteredUser(ctx, CreateRegisteredUserInput{
		ID:                    userID,
		AccountName:           input.AccountName,
		AccountNameNormalized: prepared.AccountNameNormalized,
		Nickname:              input.AccountName,
		PasswordHash:          prepared.PasswordHash,
		EmailNormalized:       normalizedEmail,
		EmailCiphertext:       emailCiphertext,
		EmailVerifiedAt:       now,
		AdultConfirmedAt:      now,
		TermsVersion:          input.TermsVersion,
		RecoveryCodeDigest:    prepared.RecoveryCodeDigest,
		InitialLight:          granted,
		InitialCoverAllowance: s.registration.InitialCoverAllowance,
		InitialSession:        session,
	})
	if createErr != nil {
		if errors.Is(createErr, ErrAccountNameTaken) || errors.Is(createErr, ErrEmailAlreadyRegistered) {
			return RegistrationResult{}, createErr
		}
		return RegistrationResult{}, createErr
	}
	_ = s.repository.CreateRegistrationRiskEvent(ctx, RegistrationRiskEvent{
		EmailHash:       emailHash,
		IPPrefixHash:    s.secrets.IPPrefixHash(input.IPPrefix),
		RiskCookieHash:  riskCookieDigest,
		ASN:             "",
		TurnstileResult: "success",
		RiskLevel:       decision.RiskLevel,
		Action:          string(decision.Action),
		GrantGranted:    granted > 0,
		CreatedAt:       now,
	})
	return RegistrationResult{
		Account: Account{
			ID:                    userID,
			AccountName:           input.AccountName,
			AccountNameNormalized: prepared.AccountNameNormalized,
			PasswordHash:          prepared.PasswordHash,
			RecoveryCodeDigest:    prepared.RecoveryCodeDigest,
			Status:                "active",
		},
		Session:      session,
		SessionToken: token,
		RecoveryCode: prepared.RecoveryCode,
		GrantedLight: granted,
	}, nil
}

type RegisterInput struct {
	AccountName    string
	Password       string
	Email          string
	Code           string
	Nonce          string
	TurnstileToken string
	AdultConfirmed bool
	TermsVersion   string
	RemoteIP       string
	RiskCookie     string
	IPPrefix       string
}

func randomNonce(size int) []byte {
	nonce := make([]byte, size)
	_, _ = rand.Read(nonce)
	return nonce
}

var _ = fmt.Sprintf
