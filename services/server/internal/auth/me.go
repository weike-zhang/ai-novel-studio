package auth

import (
	"context"
	"time"
)

// CurrentUser is the authenticated identity and entitlement summary returned
// by GET /me. The email is masked: it is never the full display value and
// never usable as a credential.
type CurrentUser struct {
	UserID              string
	AccountName         string
	Nickname            string
	Email               string
	EmailVerified       bool
	AdultConfirmed      bool
	TermsVersion        string
	Status              string
	AvailableBalance    int64
	CreatedAt           time.Time
	RecoveryCodeSet     bool
}

type SessionSummary struct {
	ID        string
	FamilyID  string
	CreatedAt time.Time
	LastUsedAt time.Time
	RevokedAt *time.Time
	Current   bool
}

func (s *Service) Me(ctx context.Context, userID string) (CurrentUser, error) {
	account, err := s.repository.FindAccountByID(ctx, userID)
	if err != nil {
		return CurrentUser{}, err
	}
	balance, err := s.repository.WalletBalance(ctx, userID)
	if err != nil {
		return CurrentUser{}, err
	}
	email := ""
	if account.EmailCiphertext != nil {
		if plain, openErr := s.emailCipher.OpenEmail(account.EmailCiphertext); openErr == nil {
			email = maskEmail(plain)
		} else {
			email = maskEmail(account.EmailNormalized)
		}
	}
	return CurrentUser{
		UserID:           account.ID,
		AccountName:      account.AccountName,
		Nickname:         account.Nickname,
		Email:            email,
		EmailVerified:    account.EmailVerifiedAt != nil,
		AdultConfirmed:   account.AdultConfirmedAt != nil,
		TermsVersion:     account.TermsVersion,
		Status:           account.Status,
		AvailableBalance: balance,
		CreatedAt:        account.CreatedAt,
		RecoveryCodeSet:  len(account.RecoveryCodeDigest) > 0,
	}, nil
}

func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]SessionSummary, error) {
	sessions, err := s.repository.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	summaries := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session.RevokedAt != nil {
			continue
		}
		summaries = append(summaries, SessionSummary{
			ID:         session.ID,
			FamilyID:   session.FamilyID,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
			Current:    session.ID == currentSessionID,
		})
	}
	return summaries, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.repository.RevokeSessionByID(ctx, sessionID, userID, s.now().UTC())
}

func maskEmail(email string) string {
	at := indexByte(email, '@')
	if at < 0 {
		return "***"
	}
	local, domain := email[:at], email[at:]
	runes := []rune(local)
	switch len(runes) {
	case 0:
		return "***" + domain
	case 1:
		return "***" + domain
	case 2:
		return string(runes[0]) + "**" + domain
	default:
		return string(runes[0]) + "***" + string(runes[len(runes)-1]) + domain
	}
}

func indexByte(value string, target byte) int {
	for index := 0; index < len(value); index++ {
		if value[index] == target {
			return index
		}
	}
	return -1
}
