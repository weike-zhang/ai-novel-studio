package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultSessionCookieName = "novelstudio_session"
	riskCookieName           = "novelstudio_risk"
	defaultSessionLifetime   = 20 * 24 * time.Hour
	defaultMaxRequestBytes   = 16 << 10
)

// HTTPConfig controls transport-only authentication settings. Session tokens
// are always carried in an HttpOnly cookie and are never encoded in JSON.
type HTTPConfig struct {
	Production      bool
	CookieName      string
	SessionLifetime time.Duration
	MaxRequestBytes int64
	// EchoVerificationCode returns the registration code in the response. It is
	// strictly a development convenience for the synthetic email provider and
	// must never be enabled in production.
	EchoVerificationCode bool
}

func DefaultHTTPConfig(production bool) HTTPConfig {
	return HTTPConfig{
		Production:      production,
		CookieName:      defaultSessionCookieName,
		SessionLifetime: defaultSessionLifetime,
		MaxRequestBytes: defaultMaxRequestBytes,
	}
}

type HTTPHandler struct {
	service *Service
	config  HTTPConfig
}

func NewHTTPHandler(service *Service, config HTTPConfig) (*HTTPHandler, error) {
	if service == nil || config.SessionLifetime <= 0 || config.MaxRequestBytes <= 0 {
		return nil, ErrInvalidConfiguration
	}
	config.CookieName = strings.TrimSpace(config.CookieName)
	if config.CookieName == "" || !validCookieName(config.CookieName) {
		return nil, ErrInvalidConfiguration
	}
	return &HTTPHandler{service: service, config: config}, nil
}

// EnableDevelopmentVerificationCodeEcho turns on development-only echo of the
// registration code. It must only be called in development environments.
func (h *HTTPHandler) EnableDevelopmentVerificationCodeEcho() {
	h.config.EchoVerificationCode = true
}

type principalContextKey struct{}

type Principal struct {
	UserID    string
	SessionID string
	FamilyID  string
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok && principal.UserID != "" && principal.SessionID != "" && principal.FamilyID != ""
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	principal, ok := PrincipalFromContext(ctx)
	return principal.UserID, ok
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch path {
	case "/v1/auth/login":
		h.requireMethod(w, r, http.MethodPost, h.login)
	case "/v1/auth/logout":
		h.requireMethod(w, r, http.MethodPost, h.authenticated(h.logout).ServeHTTP)
	case "/v1/auth/logout-all":
		h.requireMethod(w, r, http.MethodPost, h.authenticated(h.logoutAll).ServeHTTP)
	case "/v1/auth/password/recover":
		h.requireMethod(w, r, http.MethodPost, h.recoverPassword)
	case "/v1/auth/email-verification-codes":
		h.requireMethod(w, r, http.MethodPost, h.emailVerificationCodes)
	case "/v1/auth/register":
		h.requireMethod(w, r, http.MethodPost, h.register)
	case "/v1/auth/password/change":
		h.requireMethod(w, r, http.MethodPost, h.authenticated(h.changePassword).ServeHTTP)
	case "/v1/auth/recovery-code/rotate":
		h.requireMethod(w, r, http.MethodPost, h.authenticated(h.rotateRecoveryCode).ServeHTTP)
	case "/v1/me":
		h.requireMethod(w, r, http.MethodGet, h.authenticated(h.me).ServeHTTP)
	case "/v1/me/sessions":
		h.requireMethod(w, r, http.MethodGet, h.authenticated(h.meSessions).ServeHTTP)
	case "/v1/me/sessions/revoke":
		h.requireMethod(w, r, http.MethodPost, h.authenticated(h.revokeSession).ServeHTTP)
	default:
		writeHTTPError(w, http.StatusNotFound, "not_found", "资源不存在")
	}
}

// Authenticate validates the session cookie, rotates it when due, and places
// the authenticated principal in the request context.
func (h *HTTPHandler) Authenticate(next http.Handler) http.Handler {
	return h.authenticated(next.ServeHTTP)
}

func (h *HTTPHandler) authenticated(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated, ok := h.AuthenticateRequest(w, r)
		if !ok {
			return
		}
		next(w, authenticated)
	})
}

// AuthenticateRequest is the transport bridge used by routers that need to
// continue their own middleware chain after cookie authentication.
func (h *HTTPHandler) AuthenticateRequest(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	cookie, err := r.Cookie(h.config.CookieName)
	if err != nil || cookie.Value == "" {
		clearSessionCookie(w, h.config)
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return r, false
	}
	result, err := h.service.AuthenticateSession(r.Context(), cookie.Value)
	if err != nil {
		clearSessionCookie(w, h.config)
		if isPublicAuthenticationError(err) {
			writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
			return r, false
		}
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return r, false
	}
	if result.ReplacementToken != "" {
		setSessionCookie(w, h.config, result.ReplacementToken)
	}
	principal := Principal{UserID: result.Session.UserID, SessionID: result.Session.ID, FamilyID: result.Session.FamilyID}
	return r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)), true
}

func (h *HTTPHandler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountName string `json:"accountName"`
		Password    string `json:"password"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.AccountName == "" || input.Password == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请填写账号和密码")
		return
	}
	result, err := h.service.Login(r.Context(), input.AccountName, input.Password)
	if err != nil {
		writeAuthenticationFailure(w, err)
		return
	}
	setSessionCookie(w, h.config, result.SessionToken)
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusOK, struct {
		UserID      string `json:"userId"`
		AccountName string `json:"accountName"`
	}{UserID: result.Account.ID, AccountName: result.Account.AccountName})
}

func (h *HTTPHandler) logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	if err := h.service.LogoutFamily(r.Context(), principal.FamilyID); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	clearSessionCookie(w, h.config)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) logoutAll(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	if err := h.service.LogoutAll(r.Context(), principal.UserID); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	clearSessionCookie(w, h.config)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) recoverPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountName         string `json:"accountName"`
		RecoveryCode        string `json:"recoveryCode"`
		ReplacementPassword string `json:"replacementPassword"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.AccountName == "" || input.RecoveryCode == "" || input.ReplacementPassword == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请填写完整恢复信息")
		return
	}
	newRecoveryCode, err := h.service.RecoverPassword(r.Context(), input.AccountName, input.RecoveryCode, input.ReplacementPassword)
	if err != nil {
		writeAuthenticationFailure(w, err)
		return
	}
	clearSessionCookie(w, h.config)
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusOK, struct {
		RecoveryCode string `json:"recoveryCode"`
	}{RecoveryCode: newRecoveryCode})
}

func (h *HTTPHandler) emailVerificationCodes(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email          string `json:"email"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.Email == "" || input.TurnstileToken == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请填写邮箱并完成人机验证")
		return
	}
	inputValues := SendRegistrationCodeInput{
		Email:          input.Email,
		TurnstileToken: input.TurnstileToken,
		RemoteIP:       clientIP(r),
		RiskCookie:     h.ensureRiskCookie(w, r),
		IPPrefix:       ipPrefix(clientIP(r)),
	}
	var nonce string
	var cooldown time.Duration
	var code string
	var err error
	if h.config.EchoVerificationCode {
		nonce, cooldown, code, err = h.service.SendRegistrationCodeWithEcho(r.Context(), inputValues)
	} else {
		nonce, cooldown, err = h.service.SendRegistrationCode(r.Context(), inputValues)
	}
	if err != nil {
		writeRegistrationFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusAccepted, struct {
		Nonce            string `json:"nonce"`
		CooldownSeconds  int    `json:"cooldownSeconds"`
		VerificationCode string `json:"verificationCode,omitempty"`
	}{Nonce: nonce, CooldownSeconds: int(cooldown / time.Second), VerificationCode: code})
}

func (h *HTTPHandler) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountName    string `json:"accountName"`
		Password       string `json:"password"`
		Email          string `json:"email"`
		Code           string `json:"code"`
		Nonce          string `json:"nonce"`
		TurnstileToken string `json:"turnstileToken"`
		AdultConfirmed bool   `json:"adultConfirmed"`
		TermsVersion   string `json:"termsVersion"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	result, err := h.service.Register(r.Context(), RegisterInput{
		AccountName:    input.AccountName,
		Password:       input.Password,
		Email:          input.Email,
		Code:           input.Code,
		Nonce:          input.Nonce,
		TurnstileToken: input.TurnstileToken,
		AdultConfirmed: input.AdultConfirmed,
		TermsVersion:   input.TermsVersion,
		RemoteIP:       clientIP(r),
		RiskCookie:     h.ensureRiskCookie(w, r),
		IPPrefix:       ipPrefix(clientIP(r)),
	})
	if err != nil {
		writeRegistrationFailure(w, err)
		return
	}
	setSessionCookie(w, h.config, result.SessionToken)
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusCreated, struct {
		UserID        string `json:"userId"`
		AccountName   string `json:"accountName"`
		RecoveryCode  string `json:"recoveryCode"`
		GrantedLight  int64  `json:"grantedLight"`
	}{UserID: result.Account.ID, AccountName: result.Account.AccountName, RecoveryCode: result.RecoveryCode, GrantedLight: result.GrantedLight})
}

func (h *HTTPHandler) changePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	var input struct {
		CurrentPassword    string `json:"currentPassword"`
		ReplacementPassword string `json:"replacementPassword"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.CurrentPassword == "" || input.ReplacementPassword == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请填写当前密码和新密码")
		return
	}
	account, err := h.service.repository.FindAccountByID(r.Context(), principal.UserID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	if err := h.service.ChangePasswordKeepSession(r.Context(), account, input.CurrentPassword, input.ReplacementPassword, principal.SessionID); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeHTTPError(w, http.StatusUnauthorized, "authentication_failed", "当前密码不正确")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) rotateRecoveryCode(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.CurrentPassword == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请填写当前密码")
		return
	}
	account, err := h.service.repository.FindAccountByID(r.Context(), principal.UserID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	code, err := h.service.RotateRecoveryCode(r.Context(), account, input.CurrentPassword)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeHTTPError(w, http.StatusUnauthorized, "authentication_failed", "当前密码不正确")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusOK, struct {
		RecoveryCode string `json:"recoveryCode"`
	}{RecoveryCode: code})
}

func (h *HTTPHandler) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	current, err := h.service.Me(r.Context(), principal.UserID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusOK, struct {
		UserID           string    `json:"userId"`
		AccountName      string    `json:"accountName"`
		Nickname         string    `json:"nickname"`
		Email            string    `json:"email"`
		EmailVerified    bool      `json:"emailVerified"`
		AdultConfirmed   bool      `json:"adultConfirmed"`
		TermsVersion     string    `json:"termsVersion"`
		Status           string    `json:"status"`
		AvailableBalance int64     `json:"availableBalance"`
		CreatedAt        time.Time `json:"createdAt"`
		RecoveryCodeSet  bool      `json:"recoveryCodeSet"`
	}{
		UserID: current.UserID, AccountName: current.AccountName, Nickname: current.Nickname,
		Email: current.Email, EmailVerified: current.EmailVerified, AdultConfirmed: current.AdultConfirmed,
		TermsVersion: current.TermsVersion, Status: current.Status, AvailableBalance: current.AvailableBalance,
		CreatedAt: current.CreatedAt, RecoveryCodeSet: current.RecoveryCodeSet,
	})
}

func (h *HTTPHandler) meSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	sessions, err := h.service.ListSessions(r.Context(), principal.UserID, principal.SessionID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(w, http.StatusOK, struct {
		Sessions []SessionSummary `json:"sessions"`
	}{Sessions: sessions})
}

func (h *HTTPHandler) revokeSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	var input struct {
		SessionID string `json:"sessionId"`
	}
	if !h.decodeJSON(w, r, &input) {
		return
	}
	if input.SessionID == "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "缺少会话 ID")
		return
	}
	if input.SessionID == principal.SessionID {
		if err := h.service.LogoutFamily(r.Context(), principal.FamilyID); err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
			return
		}
		clearSessionCookie(w, h.config)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.service.RevokeSession(r.Context(), principal.UserID, input.SessionID); err != nil {
		if errors.Is(err, ErrInvalidToken) {
			writeHTTPError(w, http.StatusNotFound, "not_found", "会话不存在")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ensureRiskCookie returns the client's first-party risk cookie value, creating
// a fresh random one when absent. The cookie is not a login credential and is
// not deleted on logout so registration limits cannot be bypassed instantly.
func (h *HTTPHandler) ensureRiskCookie(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(riskCookieName)
	if err != nil || cookie.Value == "" {
		value, nonceErr := randomCookieValue()
		if nonceErr != nil {
			return ""
		}
		http.SetCookie(w, &http.Cookie{
			Name:     riskCookieName,
			Value:    value,
			Path:     "/",
			Expires:  time.Now().Add(30 * 24 * time.Hour).UTC(),
			MaxAge:   int((30 * 24 * time.Hour) / time.Second),
			HttpOnly: true,
			Secure:   h.config.Production,
			SameSite: http.SameSiteLaxMode,
		})
		return value
	}
	return cookie.Value
}

func clientIP(r *http.Request) string {
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func (h *HTTPHandler) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.config.MaxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请求体格式不正确")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func (h *HTTPHandler) requireMethod(w http.ResponseWriter, r *http.Request, method string, next http.HandlerFunc) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
		return
	}
	next(w, r)
}

func setSessionCookie(w http.ResponseWriter, config HTTPConfig, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     config.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(config.SessionLifetime).UTC(),
		MaxAge:   int(config.SessionLifetime / time.Second),
		HttpOnly: true,
		Secure:   config.Production,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, config HTTPConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     config.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   config.Production,
		SameSite: http.SameSiteLaxMode,
	})
}

func writeAuthenticationFailure(w http.ResponseWriter, err error) {
	if isPublicAuthenticationError(err) || errors.Is(err, ErrInvalidPassword) || errors.Is(err, ErrInvalidAccountName) {
		writeHTTPError(w, http.StatusUnauthorized, "authentication_failed", "账号或凭据不正确")
		return
	}
	writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
}

func writeRegistrationFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrInvalidAccountName), errors.Is(err, ErrInvalidPassword), errors.Is(err, ErrDisposableEmail):
		writeHTTPError(w, http.StatusUnprocessableEntity, "invalid_registration", err.Error())
	case errors.Is(err, ErrEmailAlreadyRegistered), errors.Is(err, ErrAccountNameTaken):
		writeHTTPError(w, http.StatusConflict, "account_exists", err.Error())
	case errors.Is(err, ErrAdultConfirmation):
		writeHTTPError(w, http.StatusUnprocessableEntity, "adult_confirmation_required", "请确认已满 18 周岁并同意条款")
	case errors.Is(err, ErrTurnstileVerification):
		writeHTTPError(w, http.StatusBadRequest, "turnstile_failed", "人机验证未通过，请重试")
	case errors.Is(err, ErrRegistrationBlocked):
		writeHTTPError(w, http.StatusForbidden, "registration_blocked", "本次注册被拒绝，如有疑问请联系我们")
	case errors.Is(err, ErrRegistrationNotAllowed):
		writeHTTPError(w, http.StatusForbidden, "registration_not_allowed", "当前不允许注册")
	case errors.Is(err, ErrVerificationCooldown):
		writeHTTPError(w, http.StatusTooManyRequests, "verification_cooldown", "发送过于频繁，请稍后再试")
	case errors.Is(err, ErrVerificationRateLimited):
		writeHTTPError(w, http.StatusTooManyRequests, "verification_rate_limited", "该邮箱发送次数过多，请明天再试")
	case errors.Is(err, ErrVerificationCode), errors.Is(err, ErrVerificationCodeUsed), errors.Is(err, ErrVerificationCodeExpired):
		writeHTTPError(w, http.StatusUnprocessableEntity, "invalid_verification_code", "验证码错误或已失效")
	default:
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func randomCookieValue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func isPublicAuthenticationError(err error) bool {
	return errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidRecoveryCode) ||
		errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrSessionRevoked) || errors.Is(err, ErrSessionReplay)
}

func writeHTTPError(w http.ResponseWriter, status int, code, message string) {
	writeHTTPJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}

func writeHTTPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character <= 0x20 || character >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", character) {
			return false
		}
	}
	return true
}
