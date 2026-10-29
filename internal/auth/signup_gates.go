package auth

import (
	_ "embed"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

// AGORA-406: cheap bot gates on /auth/register that need nothing new from
// the client, so old mobile builds (which can't be forced to update) keep
// working unchanged.

// registerRateLimit caps account creation per IP. Register had no throttle
// at all, and every hit sends a verification email to whatever address was
// typed, so it doubled as an email-bombing endpoint. The budget leaves room
// for a household or office behind one NAT signing up a few people.
var registerRateLimit = httprate.LimitByIP(5, time.Hour)

// disposable_email_domains.txt is the CC0 blocklist from
// github.com/disposable-email-domains/disposable-email-domains (commit
// fa96007, vendored 2026-10-28), one lowercase domain per line. Refresh it
// by downloading disposable_email_blocklist.conf over this file;
// TestNeverBlockedDomainsSurviveBlocklist guards the popular providers
// against a bad refresh.
//
//go:embed disposable_email_domains.txt
var disposableDomainsRaw string

var disposableDomains = parseDomainList(disposableDomainsRaw)

// neverBlockedDomains are mainstream providers real people sign up with.
// They win over the vendored disposable list, so an upstream mistake can't
// lock out gmail or proton users. An admin's own blocked_email_domains
// setting still applies to them, since that's a deliberate local choice.
var neverBlockedDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true,
	"yahoo.com": true, "yahoo.co.uk": true, "yahoo.ca": true, "yahoo.com.au": true,
	"yahoo.co.in": true, "yahoo.fr": true, "yahoo.de": true, "yahoo.co.jp": true,
	"ymail.com": true, "rocketmail.com": true,
	"proton.me": true, "protonmail.com": true, "protonmail.ch": true, "pm.me": true,
	"outlook.com": true, "hotmail.com": true, "hotmail.co.uk": true, "live.com": true, "msn.com": true,
	"icloud.com": true, "me.com": true, "mac.com": true,
	"aol.com": true, "gmx.com": true, "gmx.net": true, "gmx.de": true, "web.de": true,
	"mail.com": true, "zoho.com": true, "fastmail.com": true, "fastmail.fm": true,
	"tutanota.com": true, "tuta.io": true, "tuta.com": true, "hey.com": true,
	"yandex.com": true, "yandex.ru": true, "mail.ru": true,
	"comcast.net": true, "att.net": true, "verizon.net": true, "sbcglobal.net": true,
}

func parseDomainList(raw string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t'
	}) {
		d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(line)), "@")
		if d != "" && !strings.HasPrefix(d, "#") {
			out[d] = true
		}
	}
	return out
}

// emailDomainBlocked reports whether an address's domain, or any parent of
// it (so x.mailinator.com is caught too), is on the admin's own list or on
// the disposable list. adminBlocked is the raw blocked_email_domains setting.
func emailDomainBlocked(email, adminBlocked string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(strings.TrimSuffix(email[at+1:], "."))
	admin := parseDomainList(adminBlocked)
	for d := domain; d != ""; {
		if admin[d] {
			return true
		}
		if neverBlockedDomains[d] {
			return false
		}
		if disposableDomains[d] {
			return true
		}
		dot := strings.Index(d, ".")
		if dot < 0 {
			break
		}
		d = d[dot+1:]
	}
	return false
}
