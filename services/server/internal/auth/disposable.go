package auth

import "strings"

// disposableEmailDomains is a small, auditable blocklist of known throwaway
// email domains. Registration refuses these outright.
var disposableEmailDomains = map[string]struct{}{
	"10minutemail.com":     {},
	"guerrillamail.com":    {},
	"guerrillamail.biz":    {},
	"mailinator.com":       {},
	"mailnesia.com":        {},
	"maildrop.cc":          {},
	"tempmail.com":         {},
	"tempmail.net":         {},
	"throwawaymail.com":    {},
	"temp-mail.org":        {},
	"yopmail.com":          {},
	"yopmail.net":          {},
	"getnada.com":          {},
	"dispostable.com":      {},
	"spam4.me":             {},
	"mohmal.com":           {},
	"mohmal.in":            {},
	"emailnator.com":       {},
	"mailto.plus":          {},
	"1secmail.com":         {},
	"trashmail.com":        {},
	"sharklasers.com":      {},
	"guerrillamail.info":   {},
	"grr.la":               {},
	"guerrillamailblock.com": {},
}

func isDisposableEmail(normalized string) bool {
	at := strings.LastIndexByte(normalized, '@')
	if at < 0 {
		return false
	}
	_, blocked := disposableEmailDomains[normalized[at+1:]]
	return blocked
}
