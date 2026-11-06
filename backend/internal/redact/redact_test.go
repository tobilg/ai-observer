package redact

import (
	"strings"
	"testing"
)

// fake builds credential-shaped test values at runtime, so the source holds
// nothing that secret scanners report as a leaked key
func fake(prefix string, n int) string {
	return prefix + strings.Repeat("Ab3x", n/4+1)[:n]
}

func TestSecrets(t *testing.T) {
	r := New(Secrets)
	cases := []struct {
		name, in, kind, secret string
	}{
		{"anthropic", "export ANTHROPIC=" + fake("sk-ant-api03-", 40), "anthropic_key", fake("sk-ant-api03-", 40)},
		{"openai", "key " + fake("sk-proj-", 48) + " used", "openai_key", fake("sk-proj-", 48)},
		{"github classic", "token " + fake("ghp_", 36), "github_token", fake("ghp_", 36)},
		{"github fine-grained", fake("github_pat_", 70), "github_token", fake("github_pat_", 70)},
		{"gitlab", fake("glpat-", 20), "gitlab_token", fake("glpat-", 20)},
		{"slack", fake("xoxb-", 30), "slack_token", fake("xoxb-", 30)},
		{"aws", "AWS_ACCESS_KEY_ID=" + "AKIA" + strings.Repeat("Z7", 8), "aws_access_key", "AKIA" + strings.Repeat("Z7", 8)},
		{"google", fake("AIza", 35), "google_api_key", fake("AIza", 35)},
		{"stripe", fake("sk_live_", 24), "stripe_key", fake("sk_live_", 24)},
		{"jwt", "eyJ" + fake("", 20) + ".eyJ" + fake("", 20) + "." + fake("", 20), "jwt", fake("", 20)},
		{"bearer header", `curl -H "Authorization: Bearer ` + fake("", 32) + `"`, "token", fake("", 32)},
		{"url credentials", "postgres://admin:" + "s3cr3tPw" + "@db.example.com:5432/app", "password", "s3cr3tPw"},
		{"env assignment", "DB_PASSWORD=" + "hunter2hunter2", "secret", "hunter2hunter2"},
		{"pwd suffix", "MYSQL_PWD=" + "s3cr3tPass9", "secret", "s3cr3tPass9"},
		{"json value", `{"client_secret": "` + fake("", 24) + `"}`, "secret", fake("", 24)},
		{"yaml value", "api_key: " + fake("k9", 20), "secret", fake("k9", 20)},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow" + fake("", 40) + "\n-----END RSA PRIVATE KEY-----", "private_key", "MIIEow"},
	}
	for _, c := range cases {
		got := r.String(c.in)
		if strings.Contains(got, c.secret) || !strings.Contains(got, "[REDACTED:"+c.kind+"]") {
			t.Errorf("%s: %q -> %q, want the secret replaced by [REDACTED:%s]", c.name, c.in, got, c.kind)
		}
	}

	// The key of an assignment and the URL around credentials stay
	if got := r.String("postgres://admin:" + "s3cr3tPw" + "@db.example.com/app"); got != "postgres://admin:[REDACTED:password]@db.example.com/app" {
		t.Errorf("url credentials -> %q", got)
	}
	if got := r.String("DB_PASSWORD=" + "hunter2hunter2"); got != "DB_PASSWORD=[REDACTED:secret]" {
		t.Errorf("assignment -> %q", got)
	}
	// A token as the user of a URL, and a known token there
	token := fake("", 32)
	if got := r.String("https://" + token + "@git.example.com/o/r.git"); got != "https://[REDACTED:token]@git.example.com/o/r.git" {
		t.Errorf("token user info -> %q", got)
	}
	if got := r.String("https://" + fake("ghp_", 36) + "@github.com/o/r"); got != "https://[REDACTED:github_token]@github.com/o/r" {
		t.Errorf("github token in a URL -> %q", got)
	}
	// Ordinary user names in URLs stay
	if got := r.String("ssh://deploy@example.com/srv"); got != "ssh://deploy@example.com/srv" {
		t.Errorf("user name in a URL -> %q", got)
	}
	// The value of a query parameter ends at the next parameter
	if got := r.String("login?password=" + "hunter2abc" + "&next=/home&user=me"); got != "login?password=[REDACTED:secret]&next=/home&user=me" {
		t.Errorf("query parameter -> %q", got)
	}
}

// Code, prose and telemetry that only resemble secrets stay as they are
func TestSecretsLeaveOrdinaryText(t *testing.T) {
	r := New(Secrets)
	for _, in := range []string{
		"password = getPassword()",
		"const apiKey = process.env.API_KEY",
		"api_key: ${{ secrets.API_KEY }}",
		"secret santa is next week",
		"max_tokens: 4096, input_tokens=12345",
		"the task-manager-service-with-a-long-name is running",
		"https://example.com/docs:install",
		"session 4f3c2a10-9b1e-4c4d-8a7e-2f6b1c9d0e11 started",
		"git@github.com:owner/repo.git",
		"mail me at someone@example.com",
		// The shell's working directories, and "@" in a query or fragment
		"PWD=/home/user/dev1",
		"pwd: /Users/me/project2",
		"OLDPWD=/tmp/build42",
		"https://api.example.com:8443?user=bob@x.io",
		"https://host/docs#ref@v2",
		"commit 9fceb02d0ae598e95dc970b74767f19372d61af8",
	} {
		if got := r.String(in); got != in {
			t.Errorf("%q -> %q, want unchanged", in, got)
		}
	}
}

func TestPersonalData(t *testing.T) {
	r := New(All)
	cases := []struct{ in, want string }{
		{"Contact: Jane.Doe@Example.com", "Contact: [REDACTED:email:"},
		{"card 4111 1111 1111 1111 on file", "card [REDACTED:card_number] on file"},
		{"IBAN DE89 3704 0044 0532 0130 00", "IBAN [REDACTED:iban]"},
		{"call +49 151 23456789 today", "call [REDACTED:phone] today"},
		{"call +1 (555) 123-4567", "call [REDACTED:phone]"},
		{"SSN 123-45-6789", "SSN [REDACTED:us_ssn]"},
		{"from 8.8.8.8 via proxy", "from [REDACTED:ip_address] via proxy"},
		// Secrets are redacted in this mode too
		{"token " + fake("ghp_", 36), "token [REDACTED:github_token]"},
	}
	for _, c := range cases {
		if got := r.String(c.in); !strings.HasPrefix(got, c.want) {
			t.Errorf("%q -> %q, want it to start with %q", c.in, got, c.want)
		}
	}

	// Pseudonyms are stable and ignore case, so per-user breakdowns work
	a, b := r.String("jane.doe@example.com"), r.String("Jane.Doe@Example.COM")
	if a != b || r.String("john@example.com") == a {
		t.Errorf("pseudonyms: %q, %q, other %q", a, b, r.String("john@example.com"))
	}

	for _, in := range []string{
		"git@github.com:owner/repo.git",
		"order 4111 1111 1111 1112",   // fails the Luhn check
		"listening on 127.0.0.1:8080", // loopback
		"host 192.168.1.10 and 10.0.0.2",
		"ts 1609459200000000000",
		"+1000 lines changed",
		"version 2.5.0 released 2026-11-05",
	} {
		if got := r.String(in); got != in {
			t.Errorf("%q -> %q, want unchanged", in, got)
		}
	}
}

func TestModes(t *testing.T) {
	secret := "token " + fake("ghp_", 36)
	var nilRedactor *Redactor
	for _, r := range []*Redactor{nilRedactor, New(Off)} {
		if r.Enabled() || r.String(secret) != secret || r.Mode() != Off {
			t.Errorf("redactor %+v changed input or reports enabled", r)
		}
	}
	if got := New(Secrets).String("a@example.com"); got != "a@example.com" {
		t.Errorf("secrets mode redacted an email: %q", got)
	}

	for in, want := range map[string]Mode{"": Secrets, "off": Off, "NONE": Off, "secrets": Secrets, "all": All, "pii": All} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("everything"); err == nil {
		t.Error("an unknown mode was accepted")
	}
}

func TestMap(t *testing.T) {
	r := New(Secrets)
	clean := map[string]string{"model": "claude-sonnet-5-5", "prompt": "fix the bug"}
	if got := r.Map(clean); &got == &clean || got["prompt"] != "fix the bug" {
		t.Errorf("clean map = %v", got)
	}
	secret := fake("ghp_", 36)
	attrs := map[string]string{"model": "x", "tool_input": "git push https://" + secret + "@github.com"}
	got := r.Map(attrs)
	if strings.Contains(got["tool_input"], secret) || got["model"] != "x" {
		t.Errorf("redacted map = %v", got)
	}
	if !strings.Contains(attrs["tool_input"], secret) {
		t.Error("the input map was modified")
	}
}

func TestDeterministic(t *testing.T) {
	r := New(All)
	in := "user a@example.com key " + fake("sk-ant-", 30)
	first, second := r.String(in), r.String(in)
	if first != second {
		t.Errorf("redaction is not deterministic: %q, %q", first, second)
	}
}

func BenchmarkRedactPrompt(b *testing.B) {
	// A typical prompt and tool output without secrets: the common case
	text := strings.Repeat("Please refactor the handler in internal/server/routes.go so that "+
		"requests are validated before they reach storage; run go test ./... afterwards.\n", 40)
	// Tool output full of numbers: counts, durations, timestamps
	numbers := strings.Repeat(`{"input_tokens": 12345, "output_tokens": 678, "duration_ms": 1520, `+
		`"timestamp": "2026-11-05T10:15:30Z", "files": ["a.go:120", "b.go:88"], "exit_code": 0}`+"\n", 40)
	for _, mode := range []Mode{Secrets, All} {
		r := New(mode)
		for name, in := range map[string]string{"prose": text, "numbers": numbers} {
			b.Run(string(mode)+"/"+name, func(b *testing.B) {
				b.SetBytes(int64(len(in)))
				for b.Loop() {
					r.String(in)
				}
			})
		}
	}
}
