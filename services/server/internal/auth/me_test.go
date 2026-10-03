package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestMeReturnsMaskedIdentityAndBalance(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	emailCipher, err := NewEmailCipher(bytes.Repeat([]byte("M"), 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterDependencies(emailCipher, newFakeCodeStore(), &recordingEmailProvider{}, &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}, defaultRegistrationRiskService(), DefaultRegistrationConfig()); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareCredentials("Me_User", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := emailCipher.SealEmail(make([]byte, 12), "me-user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	store.accounts[prepared.AccountNameNormalized] = Account{
		ID: "me-user-1", AccountName: "Me_User", AccountNameNormalized: prepared.AccountNameNormalized,
		Nickname: "Me_User", PasswordHash: prepared.PasswordHash, RecoveryCodeDigest: prepared.RecoveryCodeDigest,
		Status: "active", EmailNormalized: "me-user@example.com", EmailCiphertext: sealed,
		EmailVerifiedAt: &now,
	}
	store.balances = map[string]int64{"me-user-1": 42}

	login, err := service.Login(context.Background(), "Me_User", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	response := performAuthRequest(handler, http.MethodGet, "/v1/me", "", sessionCookie(login.SessionToken))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		UserID           string `json:"userId"`
		AccountName      string `json:"accountName"`
		Email            string `json:"email"`
		EmailVerified    bool   `json:"emailVerified"`
		AvailableBalance int64  `json:"availableBalance"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.UserID != "me-user-1" || body.AccountName != "Me_User" || body.AvailableBalance != 42 {
		t.Fatalf("me body = %#v", body)
	}
	if body.Email == "me-user@example.com" || body.Email == "" || !body.EmailVerified {
		t.Fatalf("email should be masked: %q verified=%v", body.Email, body.EmailVerified)
	}
}

func TestMeSessionsListsOnlyActiveAndMarksCurrent(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	_, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	response := performAuthRequest(handler, http.MethodGet, "/v1/me/sessions", "", sessionCookie(second.SessionToken))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("sessions = %#v", body.Sessions)
	}
	// second is current; first is not.
	current := 0
	for _, session := range body.Sessions {
		if session.Current {
			current++
			if session.ID != second.Session.ID {
				t.Fatalf("current session = %q, want %q", session.ID, second.Session.ID)
			}
		}
	}
	if current != 1 {
		t.Fatalf("current count = %d, body = %s", current, response.Body.String())
	}
}

func TestChangePasswordKeepsCurrentSessionAndRevokesOthers(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	other, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	response := performAuthRequest(handler, http.MethodPost, "/v1/auth/password/change", `{"currentPassword":"correct horse 42","replacementPassword":"replacement pass 7"}`, sessionCookie(current.SessionToken))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := service.AuthenticateSession(context.Background(), current.SessionToken); err != nil {
		t.Fatalf("current session revoked after password change: %v", err)
	}
	if _, err := service.AuthenticateSession(context.Background(), other.SessionToken); err == nil {
		t.Fatal("other session still valid after password change")
	}
	if _, err := service.Login(context.Background(), account.AccountName, "correct horse 42"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login error = %v", err)
	}
	if _, err := service.Login(context.Background(), account.AccountName, "replacement pass 7"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestRotateRecoveryCodeReturnsNewCodeOnce(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	login, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}

	response := performAuthRequest(handler, http.MethodPost, "/v1/auth/recovery-code/rotate", `{"currentPassword":"correct horse 42"}`, sessionCookie(login.SessionToken))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.RecoveryCode == "" {
		t.Fatalf("body = %q err = %v", response.Body.String(), err)
	}
	// The old recovery code from addTestAccount is a different value; verify
	// the new one can recover the account.
	if _, err := service.RecoverPassword(context.Background(), account.AccountName, body.RecoveryCode, "another pass 9"); err != nil {
		t.Fatalf("new recovery code not usable: %v", err)
	}
}
