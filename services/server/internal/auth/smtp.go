package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPEmailProvider delivers registration codes through a real SMTP relay.
// It requires an address of the form host:port and reads credentials from
// SMTP_USERNAME / SMTP_PASSWORD when present (otherwise it sends unauthenticated,
// suitable only for a trusted internal relay).
type SMTPEmailProvider struct {
	Addr     string
	Username string
	Password string
	From     string
}

var _ EmailProvider = (*SMTPEmailProvider)(nil)

func NewSMTPEmailProvider(addr, username, password, from string) (*SMTPEmailProvider, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("smtp: address is required")
	}
	if from == "" {
		return nil, fmt.Errorf("smtp: from address is required")
	}
	return &SMTPEmailProvider{Addr: addr, Username: username, Password: password, From: from}, nil
}

// encodeRFC2047 encodes a header value that may contain non-ASCII characters
// following RFC 2047 so it survives SMTP relays that reject raw UTF-8 headers.
func encodeRFC2047(value string) string {
	if isASCII(value) {
		return value
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	return "=?utf-8?B?" + encoded + "?="
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func (p *SMTPEmailProvider) SendRegistrationCode(_ context.Context, email, code string) (MessageReceipt, error) {
	host, _, err := net.SplitHostPort(p.Addr)
	if err != nil {
		host = p.Addr
	}
	messageID := generateMessageID()
	subject := encodeRFC2047("AI 小说创作台注册验证码")
	body := fmt.Sprintf(
		"你的AI 小说创作台注册验证码是：%s\r\n\r\n验证码 10 分钟内有效，且只能用于注册。如果这不是你本人的操作，请忽略本邮件。\r\n",
		code,
	)
	message := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s",
		p.From, email, subject, time.Now().UTC().Format(time.RFC1123Z), messageID,
		base64.StdEncoding.EncodeToString([]byte(body)),
	)

	var auth smtp.Auth
	if p.Username != "" {
		auth = smtp.PlainAuth("", p.Username, p.Password, host)
	}
	if err := smtp.SendMail(p.Addr, auth, p.From, []string{email}, []byte(message)); err != nil {
		return MessageReceipt{Accepted: false, Result: err.Error()}, fmt.Errorf("send registration email: %w", err)
	}
	return MessageReceipt{ProviderMessageID: messageID, Accepted: true}, nil
}

func generateMessageID() string {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:]) + "@mail.novelstudio.xyz"
}
