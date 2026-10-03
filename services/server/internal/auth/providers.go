package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MessageReceipt identifies an accepted outbound email at the provider.
type MessageReceipt struct {
	ProviderMessageID string
	Accepted          bool
	Result            string
}

// EmailProvider sends registration verification emails. The code is never
// persisted; it is rendered once into the message.
type EmailProvider interface {
	SendRegistrationCode(ctx context.Context, email, code string) (MessageReceipt, error)
}

// EmailSender provides the transport for development and production providers.
type EmailSender func(ctx context.Context, email, subject, body string) (MessageReceipt, error)

// SyntheticEmailProvider logs the code for local development; it accepts
// everything and records a synthetic message id. It must never be wired into
// production routes.
type SyntheticEmailProvider struct {
	From string
	Log  io.Writer
}

var _ EmailProvider = (*SyntheticEmailProvider)(nil)

func (p *SyntheticEmailProvider) SendRegistrationCode(ctx context.Context, email, code string) (MessageReceipt, error) {
	digest := sha256.Sum256([]byte(email))
	messageID := "synthetic-" + hex.EncodeToString(digest[:8]) + "-" + time.Now().UTC().Format("20060102150405")
	if p.Log != nil {
		_, _ = fmt.Fprintf(p.Log, "[synthetic-email] to=%s subject=AI 小说创作台注册验证码 code=%s\n", email, code)
	}
	return MessageReceipt{ProviderMessageID: messageID, Accepted: true}, nil
}

// HumanCheckProvider verifies a Cloudflare Turnstile widget token on the
// server. The client's widget result is never trusted.
type HumanCheckProvider interface {
	VerifyRegistration(ctx context.Context, token, remoteIP string) (HumanCheckResult, error)
}

type HumanCheckResult struct {
	Success  bool
	Action   string
	Hostname string
	Message  string
}

type TurnstileProvider struct {
	SecretKey   string
	SiteVerify  string
	Timeout     time.Duration
	HTTPClient  *http.Client
	ExpectedAction string
}

var _ HumanCheckProvider = (*TurnstileProvider)(nil)

func NewTurnstileProvider(secretKey, expectedAction string) (*TurnstileProvider, error) {
	if strings.TrimSpace(secretKey) == "" {
		return nil, ErrInvalidConfiguration
	}
	return &TurnstileProvider{
		SecretKey:      secretKey,
		SiteVerify:     "https://challenges.cloudflare.com/turnstile/v0/siteverify",
		Timeout:        5 * time.Second,
		HTTPClient:     &http.Client{},
		ExpectedAction: expectedAction,
	}, nil
}

func (p *TurnstileProvider) VerifyRegistration(ctx context.Context, token, remoteIP string) (HumanCheckResult, error) {
	if strings.TrimSpace(token) == "" {
		return HumanCheckResult{Success: false, Message: "missing-turnstile-token"}, nil
	}
	form := url.Values{}
	form.Set("secret", p.SecretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	requestCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, p.SiteVerify, strings.NewReader(form.Encode()))
	if err != nil {
		return HumanCheckResult{}, fmt.Errorf("turnstile request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.HTTPClient.Do(request)
	if err != nil {
		return HumanCheckResult{}, fmt.Errorf("turnstile siteverify: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return HumanCheckResult{Success: false, Message: fmt.Sprintf("turnstile-http-%d", response.StatusCode)}, nil
	}
	var body struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
		Action     string   `json:"action"`
		Hostname   string   `json:"hostname"`
		ChallengeTs string  `json:"challenge_ts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return HumanCheckResult{}, fmt.Errorf("decode turnstile response: %w", err)
	}
	if !body.Success {
		message := strings.Join(body.ErrorCodes, ",")
		return HumanCheckResult{Success: false, Action: body.Action, Hostname: body.Hostname, Message: message}, nil
	}
	if p.ExpectedAction != "" && body.Action != p.ExpectedAction {
		return HumanCheckResult{Success: false, Action: body.Action, Hostname: body.Hostname, Message: "unexpected-turnstile-action"}, nil
	}
	return HumanCheckResult{Success: true, Action: body.Action, Hostname: body.Hostname}, nil
}

// SyntheticHumanCheckProvider trusts a fixed development token, mirroring the
// local-only synthetic moderation provider. Production must not use it.
type SyntheticHumanCheckProvider struct {
	DevToken string
	Action   string
}

var _ HumanCheckProvider = (*SyntheticHumanCheckProvider)(nil)

func (p *SyntheticHumanCheckProvider) VerifyRegistration(ctx context.Context, token, _ string) (HumanCheckResult, error) {
	if token == p.DevToken {
		return HumanCheckResult{Success: true, Action: p.Action, Hostname: "localhost"}, nil
	}
	return HumanCheckResult{Success: false, Message: "invalid-synthetic-token"}, nil
}

// RegistrationRiskInput carries the signals the risk service needs to decide.
// The risk cookie value is only ever digested; the raw value stays in the
// browser.
type RegistrationRiskInput struct {
	RiskCookieDigest []byte
	IPPrefix         string
	ASN              string
	EmailHash        []byte
	EmailNormalized  string
	Disposable       bool
	Turnstile        HumanCheckResult
	AccountsPerCookie int
	Now              time.Time
}

type RiskAction string

const (
	RiskActionAllow            RiskAction = "allow"
	RiskActionAllowWithoutGrant RiskAction = "allow_without_grant"
	RiskActionChallenge        RiskAction = "challenge"
	RiskActionManualReview     RiskAction = "manual_review"
	RiskActionBlock            RiskAction = "block"
)

type RegistrationRiskDecision struct {
	Action   RiskAction
	RiskLevel string
	Reason   string
}

type RegistrationRiskService interface {
	Assess(ctx context.Context, input RegistrationRiskInput) (RegistrationRiskDecision, error)
}

// DefaultRegistrationRiskService is the soft-risk controller. No single signal
// proves an identity; thresholds only raise challenges, drop the new-user
// grant, or send a registration to manual review.
type DefaultRegistrationRiskService struct {
	MaxAccountsPerCookie int
	MaxAccountsPerIP     int
}

var _ RegistrationRiskService = (*DefaultRegistrationRiskService)(nil)

func (s *DefaultRegistrationRiskService) Assess(_ context.Context, input RegistrationRiskInput) (RegistrationRiskDecision, error) {
	if !input.Turnstile.Success {
		return RegistrationRiskDecision{Action: RiskActionBlock, RiskLevel: "high", Reason: "turnstile"}, nil
	}
	if input.Disposable {
		return RegistrationRiskDecision{Action: RiskActionBlock, RiskLevel: "high", Reason: "disposable_email"}, nil
	}
	if input.AccountsPerCookie >= s.MaxAccountsPerCookie {
		return RegistrationRiskDecision{Action: RiskActionBlock, RiskLevel: "high", Reason: "risk_cookie_limit"}, nil
	}
	if input.AccountsPerCookie > 0 {
		return RegistrationRiskDecision{Action: RiskActionAllowWithoutGrant, RiskLevel: "medium", Reason: "repeat_cookie"}, nil
	}
	if input.IPPrefix != "" {
		// IP rate is applied separately by the service using a counter.
	}
	return RegistrationRiskDecision{Action: RiskActionAllow, RiskLevel: "low", Reason: ""}, nil
}

func defaultRegistrationRiskService() *DefaultRegistrationRiskService {
	return &DefaultRegistrationRiskService{MaxAccountsPerCookie: 3, MaxAccountsPerIP: 5}
}

// ipPrefix extracts the network prefix for a client IP so only a short-lived
// HMAC is ever stored or compared. IPv4 uses the /24, IPv6 the /64.
func ipPrefix(rawIP string) string {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return (&net.IPNet{IP: v4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}).String()
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128)}).String()
}
