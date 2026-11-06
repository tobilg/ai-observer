// Package redact replaces secrets and personal data in telemetry text before
// it is stored (AI_OBSERVER_REDACT). Prompts, tool inputs and tool outputs
// that AI coding tools record often contain API keys, tokens and passwords,
// and stored telemetry is shown on the dashboard and handed to agents over
// MCP.
//
// Secrets are recognized by the formats of their issuers (for example
// "sk-ant-", "ghp_", "AKIA") and by assignments such as "password=...".
// Personal data covers email addresses, phone numbers in international
// format, payment card numbers (Luhn-checked), IBANs (checksum-checked), US
// social security numbers and public IP addresses. Names and postal
// addresses cannot be recognized reliably and are not redacted.
//
// Replacements are deterministic, so equal input stays equal (metric series
// keep their identity), and they name what was replaced, e.g.
// "[REDACTED:github_token]". Email addresses become a pseudonym with a short
// hash ("[REDACTED:email:1a2b3c4d]"), so per-user breakdowns keep working.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Mode selects what is redacted
type Mode string

const (
	// Off stores telemetry as it is received
	Off Mode = "off"
	// Secrets redacts credentials: API keys, tokens, passwords, private keys
	Secrets Mode = "secrets"
	// All redacts secrets and personal data
	All Mode = "all"
)

// DefaultMode is used when AI_OBSERVER_REDACT is not set
const DefaultMode = Secrets

// ParseMode reads AI_OBSERVER_REDACT. "" is the default mode.
func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return DefaultMode, nil
	case "off", "none", "false", "0":
		return Off, nil
	case "secrets", "secret":
		return Secrets, nil
	case "all", "pii", "secrets+pii":
		return All, nil
	default:
		return DefaultMode, fmt.Errorf("unknown redaction mode %q (off, secrets, all)", value)
	}
}

// rule replaces the matches of a pattern. hints are substrings at least one
// of which every match contains (checked case-insensitively when lowerHints):
// strings without them are skipped without running the pattern.
type rule struct {
	kind       string
	pattern    *regexp.Regexp
	hints      []string
	lowerHints bool
	// minDigits skips strings with fewer digits, for patterns of numbers
	minDigits int
	// hint, if set, replaces hints: it gets the lowercased string
	hint func(lower string) bool
	// replace returns the replacement of a match; nil replaces the whole
	// match with "[REDACTED:kind]"
	replace func(match []string) (string, bool)
}

// Redactor replaces secrets (and in mode All personal data) in strings. A nil
// Redactor and one in mode Off return their input.
type Redactor struct {
	mode  Mode
	rules []rule
}

// New returns a Redactor for mode
func New(mode Mode) *Redactor {
	r := &Redactor{mode: mode}
	switch mode {
	case Secrets:
		r.rules = secretRules
	case All:
		r.rules = append(append([]rule{}, secretRules...), personalDataRules...)
	}
	return r
}

// Mode returns the redaction mode
func (r *Redactor) Mode() Mode {
	if r == nil {
		return Off
	}
	return r.mode
}

// Enabled reports whether r redacts anything
func (r *Redactor) Enabled() bool {
	return r != nil && len(r.rules) > 0
}

// String returns s with secrets (and personal data) replaced
func (r *Redactor) String(s string) string {
	if !r.Enabled() || len(s) < 6 {
		return s
	}
	var lower string
	digits := -1
	for _, ru := range r.rules {
		if ru.minDigits > 0 {
			if digits < 0 {
				digits = countDigits(s)
			}
			if digits < ru.minDigits {
				continue
			}
		}
		if ru.hint != nil {
			if lower == "" {
				lower = strings.ToLower(s)
			}
			if !ru.hint(lower) {
				continue
			}
		} else if len(ru.hints) > 0 {
			haystack := s
			if ru.lowerHints {
				if lower == "" {
					lower = strings.ToLower(s)
				}
				haystack = lower
			}
			found := false
			for _, h := range ru.hints {
				if strings.Contains(haystack, h) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		replaced := ru.apply(s)
		if replaced != s {
			s = replaced
			lower = ""
			digits = -1
		}
	}
	return s
}

func (ru rule) apply(s string) string {
	placeholder := "[REDACTED:" + ru.kind + "]"
	if ru.replace == nil {
		return ru.pattern.ReplaceAllString(s, placeholder)
	}
	return ru.pattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := ru.pattern.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		if replacement, ok := ru.replace(groups); ok {
			return replacement
		}
		return match
	})
}

// Map returns m with its values redacted. Keys are kept. m itself is not
// modified; when nothing changes m is returned.
func (r *Redactor) Map(m map[string]string) map[string]string {
	if !r.Enabled() || len(m) == 0 {
		return m
	}
	var out map[string]string
	for k, v := range m {
		redacted := r.String(v)
		if redacted == v {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(m))
			for k2, v2 := range m {
				out[k2] = v2
			}
		}
		out[k] = redacted
	}
	if out == nil {
		return m
	}
	return out
}

// keepPrefix keeps the first group (the key of an assignment, a header name)
// and redacts the rest of the match
func keepPrefix(kind string) func([]string) (string, bool) {
	return func(g []string) (string, bool) {
		return g[1] + "[REDACTED:" + kind + "]", true
	}
}

var secretRules = []rule{
	{
		kind:    "private_key",
		pattern: regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]+ )?PRIVATE KEY( BLOCK)?-----[\s\S]*?-----END ([A-Z0-9 ]+ )?PRIVATE KEY( BLOCK)?-----`),
		hints:   []string{"PRIVATE KEY"},
	},
	{kind: "anthropic_key", pattern: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`), hints: []string{"sk-ant-"}},
	{kind: "openai_key", pattern: regexp.MustCompile(`\bsk-(proj-|svcacct-|admin-)?[A-Za-z0-9_\-]{32,}`), hints: []string{"sk-"}},
	{kind: "github_token", pattern: regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`), hints: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"}},
	{kind: "github_token", pattern: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,}\b`), hints: []string{"github_pat_"}},
	{kind: "gitlab_token", pattern: regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`), hints: []string{"glpat-"}},
	{kind: "slack_token", pattern: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`), hints: []string{"xox"}},
	{kind: "slack_webhook", pattern: regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9_/]+`), hints: []string{"hooks.slack.com"}},
	{kind: "aws_access_key", pattern: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`), hints: []string{"AKIA", "ASIA"}},
	{kind: "google_api_key", pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`), hints: []string{"AIza"}},
	{kind: "stripe_key", pattern: regexp.MustCompile(`\b(sk|rk)_(live|test)_[0-9A-Za-z]{24,}\b`), hints: []string{"_live_", "_test_"}},
	{kind: "npm_token", pattern: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`), hints: []string{"npm_"}},
	{kind: "jwt", pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`), hints: []string{"eyJ"}},
	{
		// Authorization: Bearer <token>, also in curl commands and headers
		kind:       "token",
		pattern:    regexp.MustCompile(`(?i)(\b(?:authorization|proxy-authorization)["']?\s*[:=]\s*["']?(?:bearer|basic|token)\s+)[A-Za-z0-9._~+/=\-]{12,}`),
		hints:      []string{"authorization"},
		lowerHints: true,
		replace:    keepPrefix("token"),
	},
	{
		// scheme://user:password@host and scheme://token@host. User info
		// ends before the path, query or fragment, so an "@" there (an
		// email in a query) is not taken for its end. Placeholders
		// ("[REDACTED:...]") are not user info, so they are left alone.
		kind:    "password",
		pattern: regexp.MustCompile(`(\b[a-zA-Z][a-zA-Z0-9+.\-]*://)([^/?#\s@"'<>\[\]]+)(@)`),
		hints:   []string{"://"},
		replace: func(g []string) (string, bool) {
			userinfo := g[2]
			if i := strings.IndexByte(userinfo, ':'); i >= 0 {
				if i == len(userinfo)-1 {
					return "", false
				}
				return g[1] + userinfo[:i] + ":[REDACTED:password]" + g[3], true
			}
			if len(userinfo) >= 16 && looksLikeCredential(userinfo) {
				return g[1] + "[REDACTED:token]" + g[3], true
			}
			return "", false
		},
	},
	{
		// password=..., API_KEY: "...", client_secret = '...': the value must
		// look like a credential (letters and digits), so code such as
		// password = getPassword() or apiKey = process.env.API_KEY stays.
		// The value ends at "&", which separates query parameters.
		kind:    "secret",
		pattern: regexp.MustCompile(`(?i)(\b([A-Za-z0-9_\-]*(?:password|passwd|pwd|secret|api[_\-]?key|access[_\-]?key|auth[_\-]?token|access[_\-]?token|refresh[_\-]?token|private[_\-]?key|client[_\-]?secret))["']?\s*(?::|=|:=)\s*["']?)([A-Za-z0-9_\-+/=.!@#$%^*~]{8,})`),
		hint:    assignmentHint,
		replace: func(g []string) (string, bool) {
			// PWD and OLDPWD are the shell's working directories
			if strings.EqualFold(g[2], "pwd") || strings.EqualFold(g[2], "oldpwd") || !looksLikeCredential(g[3]) {
				return "", false
			}
			return g[1] + "[REDACTED:secret]", true
		},
	},
}

// assignmentHint reports whether lower names a credential the assignment
// pattern looks for: password, secret, or a key or token qualified as api,
// access, auth, refresh, private or client (with "_", "-" or nothing between)
func assignmentHint(lower string) bool {
	if strings.Contains(lower, "pass") || strings.Contains(lower, "pwd") || strings.Contains(lower, "secret") {
		return true
	}
	for _, noun := range []string{"key", "token"} {
		for rest, offset := lower, 0; ; {
			i := strings.Index(rest, noun)
			if i < 0 {
				break
			}
			before := lower[:offset+i]
			before = strings.TrimRight(before, "_-")
			for _, word := range credentialQualifiers {
				if strings.HasSuffix(before, word) {
					return true
				}
			}
			offset += i + len(noun)
			rest = lower[offset:]
		}
	}
	return false
}

var credentialQualifiers = []string{"api", "access", "auth", "refresh", "private", "client"}

// looksLikeCredential tells credential values from identifiers and code:
// they contain a digit and a letter, and no dots between identifiers
func looksLikeCredential(v string) bool {
	if strings.HasPrefix(v, "[REDACTED") {
		return false
	}
	var letter, digit bool
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letter = true
		}
	}
	if !letter || !digit {
		return false
	}
	// object.property chains (config.apiKey2) are code, not values
	if strings.Count(v, ".") > 0 && !strings.ContainsAny(v, "=+/!@#$%^&*~") {
		parts := strings.Split(v, ".")
		identifiers := 0
		for _, p := range parts {
			if identPattern.MatchString(p) {
				identifiers++
			}
		}
		if identifiers == len(parts) {
			return false
		}
	}
	return true
}

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var personalDataRules = []rule{
	{
		kind:    "email",
		pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}\b`),
		hints:   []string{"@"},
		replace: func(g []string) (string, bool) {
			local := strings.ToLower(g[0][:strings.IndexByte(g[0], '@')])
			// git@github.com:owner/repo is an SSH remote, not an address
			if local == "git" {
				return "", false
			}
			sum := sha256.Sum256([]byte(strings.ToLower(g[0])))
			return "[REDACTED:email:" + hex.EncodeToString(sum[:4]) + "]", true
		},
	},
	{
		// Payment card numbers: card prefixes and lengths, Luhn checksum
		kind:      "card_number",
		minDigits: 15,
		pattern:   regexp.MustCompile(`\b(?:4\d{3}|5[1-5]\d{2}|2[2-7]\d{2}|3[47]\d{2}|6011|65\d{2})(?:[ \-]?\d{4}){2}[ \-]?\d{3,4}\b`),
		replace: func(g []string) (string, bool) {
			digits := onlyDigits(g[0])
			if (len(digits) != 15 && len(digits) != 16) || !luhn(digits) {
				return "", false
			}
			return "[REDACTED:card_number]", true
		},
	},
	{
		kind:      "iban",
		minDigits: 12,
		pattern:   regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,3})?\b`),
		replace: func(g []string) (string, bool) {
			if !validIBAN(strings.ReplaceAll(g[0], " ", "")) {
				return "", false
			}
			return "[REDACTED:iban]", true
		},
	},
	{
		// International format (+country code) only: plain digit runs in
		// telemetry are mostly IDs, counts and timestamps
		kind:      "phone",
		minDigits: 8,
		pattern:   regexp.MustCompile(`\+[1-9]\d{0,2}[ \-.]?\(?\d{1,4}\)?(?:[ \-.]?\d{2,5}){1,4}\b`),
		hints:     []string{"+"},
		replace: func(g []string) (string, bool) {
			if n := len(onlyDigits(g[0])); n < 8 || n > 15 {
				return "", false
			}
			return "[REDACTED:phone]", true
		},
	},
	{kind: "us_ssn", minDigits: 9, pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), hints: []string{"-"}},
	{
		// Public IPv4 addresses; loopback, private and link-local ones are
		// kept (they identify no one and show up in every local setup)
		kind:      "ip_address",
		minDigits: 4,
		pattern:   regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
		hints:     []string{"."},
		replace: func(g []string) (string, bool) {
			addr, err := netip.ParseAddr(g[0])
			if err != nil || !addr.Is4() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
				addr.IsUnspecified() || addr.IsMulticast() {
				return "", false
			}
			return "[REDACTED:ip_address]", true
		},
	},
}

func countDigits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			n++
		}
	}
	return n
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func luhn(digits string) bool {
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// validIBAN checks the ISO 13616 mod-97 checksum
func validIBAN(iban string) bool {
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	remainder := 0
	for _, c := range rearranged {
		switch {
		case c >= '0' && c <= '9':
			remainder = (remainder*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			v := int(c-'A') + 10
			remainder = (remainder*100 + v) % 97
		default:
			return false
		}
	}
	return remainder == 1
}
