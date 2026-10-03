package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPLoginSetsPersistentHttpOnlyCookieWithoutTokenJSON(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")

	response := performAuthRequest(handler, http.MethodPost, "/v1/auth/login", `{"accountName":"Alice_01","password":"correct horse 42"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != defaultSessionCookieName || cookie.Value == "" || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge <= 0 || cookie.Expires.IsZero() {
		t.Fatalf("session cookie = %#v", cookie)
	}
	if strings.Contains(response.Body.String(), cookie.Value) || strings.Contains(response.Body.String(), "sessionToken") || strings.Contains(response.Body.String(), "token") {
		t.Fatalf("response leaked session token: %s", response.Body.String())
	}
	var body struct {
		UserID      string `json:"userId"`
		AccountName string `json:"accountName"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.UserID != account.ID || body.AccountName != account.AccountName {
		t.Fatalf("body = %#v, err = %v", body, err)
	}
}

func TestHTTPProductionCookieIsSecure(t *testing.T) {
	handler, service, store := testHTTPHandler(t, true)
	addTestAccount(t, service, store, "Alice_01", "correct horse 42")

	response := performAuthRequest(handler, http.MethodPost, "/v1/auth/login", `{"accountName":"Alice_01","password":"correct horse 42"}`, nil)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 || !response.Result().Cookies()[0].Secure {
		t.Fatalf("production response cookie = %#v", response.Result().Cookies())
	}
}

func TestAuthenticateMiddlewareAddsPrincipalAndRotatesCookie(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.config.SessionRotationInterval = time.Hour
	login, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)

	protected := handler.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || principal.UserID != account.ID || principal.SessionID == login.Session.ID {
			t.Fatalf("principal = %#v, ok = %v", principal, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	response := performAuthRequest(protected, http.MethodGet, "/protected", "", sessionCookie(login.SessionToken))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" || cookies[0].Value == login.SessionToken {
		t.Fatalf("rotation cookies = %#v", cookies)
	}
	if _, err := service.AuthenticateSession(context.Background(), cookies[0].Value); err != nil {
		t.Fatalf("replacement cookie is not valid: %v", err)
	}
}

func TestAuthenticateMiddlewareRejectsMissingAndInvalidCookieUniformly(t *testing.T) {
	handler, _, _ := testHTTPHandler(t, false)
	protected := handler.Authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler must not run")
	}))

	missing := performAuthRequest(protected, http.MethodGet, "/protected", "", nil)
	invalid := performAuthRequest(protected, http.MethodGet, "/protected", "", sessionCookie("not-a-valid-token"))
	if missing.Code != http.StatusUnauthorized || invalid.Code != http.StatusUnauthorized || missing.Body.String() != invalid.Body.String() {
		t.Fatalf("missing = %d %q, invalid = %d %q", missing.Code, missing.Body.String(), invalid.Code, invalid.Body.String())
	}
	for _, response := range []*httptest.ResponseRecorder{missing, invalid} {
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
			t.Fatalf("clearing cookie = %#v", cookies)
		}
	}
}

func TestHTTPLogoutAndLogoutAllRevokeSessionsAndClearCookie(t *testing.T) {
	t.Run("logout current session", func(t *testing.T) {
		handler, service, store := testHTTPHandler(t, false)
		account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
		login, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
		if err != nil {
			t.Fatal(err)
		}
		response := performAuthRequest(handler, http.MethodPost, "/v1/auth/logout", "", sessionCookie(login.SessionToken))
		if response.Code != http.StatusNoContent || response.Result().Cookies()[0].MaxAge != -1 {
			t.Fatalf("logout = %d, cookies %#v", response.Code, response.Result().Cookies())
		}
		if _, err := service.AuthenticateSession(context.Background(), login.SessionToken); !errors.Is(err, ErrSessionRevoked) {
			t.Fatalf("session after logout error = %v", err)
		}
	})

	t.Run("logout all sessions", func(t *testing.T) {
		handler, service, store := testHTTPHandler(t, false)
		account := addTestAccount(t, service, store, "Alice_01", "correct horse 42")
		first, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
		if err != nil {
			t.Fatal(err)
		}
		second, err := service.Login(context.Background(), account.AccountName, "correct horse 42")
		if err != nil {
			t.Fatal(err)
		}
		response := performAuthRequest(handler, http.MethodPost, "/v1/auth/logout-all", "", sessionCookie(first.SessionToken))
		if response.Code != http.StatusNoContent {
			t.Fatalf("logout all = %d, body = %s", response.Code, response.Body.String())
		}
		for _, token := range []string{first.SessionToken, second.SessionToken} {
			if _, err := service.AuthenticateSession(context.Background(), token); !errors.Is(err, ErrSessionRevoked) {
				t.Fatalf("session after logout-all error = %v", err)
			}
		}
	})
}

func TestHTTPPasswordRecoveryIsNonEnumerableAndReturnsOnlyNewRecoveryCode(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	prepared, err := service.PrepareCredentials("Alice_01", "correct horse 42")
	if err != nil {
		t.Fatal(err)
	}
	store.accounts[prepared.AccountNameNormalized] = Account{
		ID: "user-1", AccountName: "Alice_01", AccountNameNormalized: prepared.AccountNameNormalized,
		PasswordHash: prepared.PasswordHash, RecoveryCodeDigest: prepared.RecoveryCodeDigest, Status: "active",
	}

	unknown := performAuthRequest(handler, http.MethodPost, "/v1/auth/password/recover", `{"accountName":"Nobody_1","recoveryCode":"wrong","replacementPassword":"replacement pass 7"}`, nil)
	wrong := performAuthRequest(handler, http.MethodPost, "/v1/auth/password/recover", `{"accountName":"Alice_01","recoveryCode":"wrong","replacementPassword":"replacement pass 7"}`, nil)
	if unknown.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized || unknown.Body.String() != wrong.Body.String() {
		t.Fatalf("unknown = %d %q, wrong = %d %q", unknown.Code, unknown.Body.String(), wrong.Code, wrong.Body.String())
	}

	success := performAuthRequest(handler, http.MethodPost, "/v1/auth/password/recover", `{"accountName":"Alice_01","recoveryCode":"`+prepared.RecoveryCode+`","replacementPassword":"replacement pass 7"}`, nil)
	if success.Code != http.StatusOK || success.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("success = %d %q", success.Code, success.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(success.Body.Bytes(), &body); err != nil || body["recoveryCode"] == "" || len(body) != 1 {
		t.Fatalf("recovery body = %#v, err = %v", body, err)
	}
	if strings.Contains(success.Body.String(), "session") || strings.Contains(success.Body.String(), "token") {
		t.Fatalf("recovery response leaked session material: %s", success.Body.String())
	}
}

func TestHTTPRejectsUnknownFieldsMultipleJSONValuesAndWrongMethods(t *testing.T) {
	handler, _, _ := testHTTPHandler(t, false)
	for _, body := range []string{
		`{"accountName":"Alice_01","password":"correct horse 42","extra":true}`,
		`{"accountName":"Alice_01","password":"correct horse 42"}{}`,
	} {
		response := performAuthRequest(handler, http.MethodPost, "/v1/auth/login", body, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d", body, response.Code)
		}
	}
	response := performAuthRequest(handler, http.MethodGet, "/v1/auth/login", "", nil)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("wrong method = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
	}
}

func TestHTTPRegistrationFlowSetsRiskCookieAndSession(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	send := performAuthRequest(handler, http.MethodPost, "/v1/auth/email-verification-codes", `{"email":"alice@example.com","turnstileToken":"dev-token"}`, nil)
	if send.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, body = %s", send.Code, send.Body.String())
	}
	var sendBody struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(send.Body.Bytes(), &sendBody); err != nil || sendBody.Nonce == "" {
		t.Fatalf("send body = %q err = %v", send.Body.String(), err)
	}
	var riskCookie *http.Cookie
	for _, cookie := range send.Result().Cookies() {
		if cookie.Name == riskCookieName {
			riskCookie = cookie
		}
	}
	if riskCookie == nil || riskCookie.Value == "" || !riskCookie.HttpOnly || riskCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("risk cookie = %#v", send.Result().Cookies())
	}

	code := emailProvider.sent[0].code
	registerBody := `{"accountName":"Alice_01","password":"correct horse 42","email":"alice@example.com","code":"` + code + `","nonce":"` + sendBody.Nonce + `","turnstileToken":"dev-token","adultConfirmed":true,"termsVersion":"v1"}`
	register := performAuthRequest(handler, http.MethodPost, "/v1/auth/register", registerBody, riskCookie)
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", register.Code, register.Body.String())
	}
	var registerResult struct {
		UserID       string `json:"userId"`
		AccountName  string `json:"accountName"`
		RecoveryCode string `json:"recoveryCode"`
		GrantedLight int64  `json:"grantedLight"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &registerResult); err != nil || registerResult.UserID == "" || registerResult.RecoveryCode == "" || registerResult.AccountName != "Alice_01" {
		t.Fatalf("register body = %q err = %v", register.Body.String(), err)
	}
	cookies := register.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == defaultSessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("session cookie = %#v", cookies)
	}
	if _, err := service.AuthenticateSession(context.Background(), sessionCookie.Value); err != nil {
		t.Fatalf("registered session not usable: %v", err)
	}
}

func TestHTTPRegistrationFlowRejectsUnknownFieldsAndMissingConfirmation(t *testing.T) {
	handler, service, store := testHTTPHandler(t, false)
	emailProvider := &recordingEmailProvider{}
	humanCheck := &SyntheticHumanCheckProvider{DevToken: "dev-token", Action: "register"}
	attachRegistration(t, service, store, emailProvider, humanCheck)

	extra := performAuthRequest(handler, http.MethodPost, "/v1/auth/register", `{"accountName":"Alice_01","password":"correct horse 42","email":"a@example.com","extra":true}`, nil)
	if extra.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", extra.Code)
	}
	send := performAuthRequest(handler, http.MethodPost, "/v1/auth/email-verification-codes", `{"email":"bob@example.com","turnstileToken":"dev-token"}`, nil)
	if send.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, body = %s", send.Code, send.Body.String())
	}
	var sendBody struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(send.Body.Bytes(), &sendBody); err != nil {
		t.Fatal(err)
	}
	code := emailProvider.sent[0].code
	missing := performAuthRequest(handler, http.MethodPost, "/v1/auth/register", `{"accountName":"Bob_01","password":"correct horse 42","email":"bob@example.com","code":"`+code+`","nonce":"`+sendBody.Nonce+`","turnstileToken":"dev-token","adultConfirmed":false,"termsVersion":"v1"}`, nil)
	if missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing confirmation status = %d, body = %s", missing.Code, missing.Body.String())
	}
}

func testHTTPHandler(t *testing.T, production bool) (*HTTPHandler, *Service, *fakeRepository) {
	t.Helper()
	store := newFakeRepository()
	service := testService(t, store)
	handler, err := NewHTTPHandler(service, DefaultHTTPConfig(production))
	if err != nil {
		t.Fatal(err)
	}
	return handler, service, store
}

func sessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: defaultSessionCookieName, Value: token, Path: "/"}
}

func performAuthRequest(handler http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
