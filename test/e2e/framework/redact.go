package framework

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// RedactPlaceholder replaces every redacted sensitive value in console output
// and diagnostic artifacts.
const RedactPlaceholder = "[REDACTED]"

// sensitiveKeys enumerates map keys whose values are credentials or credential
// material, matched exactly (token, password, client-key-data, ...).
const sensitiveKeys = `token|tokens|password|passwd|passphrase|secret|apikey|api-key|client-key-data|client-certificate-data|certificate-authority-data|bearerToken|bearer-token|username`

// sensitiveKeySegments enumerates key segments such that a hyphen/dot/underscore
// separated key ending in one of them is credential-shaped. This catches
// production configuration keys (prometheus-password, prometheus-bearer-token)
// and Secret data keys (tls.key) without matching ordinary camelCase fields
// (secretName, serviceAccountName).
const sensitiveKeySegments = `token|tokens|password|passwd|passphrase|secret|apikey|bearer|key|username`

// sensitiveKeyPattern matches a whole map key that is either exactly one of
// the sensitive keys or a segment-separated key containing a sensitive segment.
// The segment alternation must be its own group or the outer alternation would
// fragment it (only prefix+token would remain a valid alternative).
const sensitiveKeyPattern = `(?:` + sensitiveKeys + `|(?:[A-Za-z0-9]+[-_.])+(?:` + sensitiveKeySegments + `)(?:[-_.][A-Za-z0-9]+)*)`

var redactors = []struct {
	name  string
	re    *regexp.Regexp
	templ string
}{
	{
		// PEM private-key blocks of any type, including single-line values with
		// escaped newlines (how Kubernetes serialises multi-line annotations).
		name:  "private-key-pem",
		re:    regexp.MustCompile(`(?s)-----BEGIN ((?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY)-----.*?-----END (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`),
		templ: "-----BEGIN $1-----" + RedactPlaceholder + "-----END $1-----",
	},
	{
		// Authorization headers and inline bearer credentials.
		name:  "bearer-token",
		re:    regexp.MustCompile(`(?i)\bbearer[ \t]+[A-Za-z0-9._~+/=-]{8,}`),
		templ: "Bearer " + RedactPlaceholder,
	},
	{
		// Service-account / OIDC JWTs wherever they appear.
		name:  "jwt",
		re:    regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`),
		templ: RedactPlaceholder,
	},
	{
		// Credentials embedded in URLs (https://user:pass@host). // pragma: allowlist secret
		name:  "url-userinfo",
		re:    regexp.MustCompile(`(?i)(https?|ftp)://[^/\s:@]+:[^@\s]+@`),
		templ: "$1://" + RedactPlaceholder + "@",
	},
	{
		// YAML mapping values under sensitive keys.
		name:  "sensitive-yaml-value",
		re:    regexp.MustCompile(`(?im)^([ \t]*"?` + sensitiveKeyPattern + `"?[ \t]*:[ \t]+).+$`),
		templ: "${1}" + RedactPlaceholder,
	},
	{
		// JSON mapping values under sensitive keys. Value classes exclude markup
		// characters so scrubbing XML-embedded content cannot swallow tags.
		name:  "sensitive-json-value",
		re:    regexp.MustCompile(`(?i)("` + sensitiveKeyPattern + `"[ \t]*:[ \t]*)"[^"<>&]*"`),
		templ: `${1}"` + RedactPlaceholder + `"`,
	},
	{
		// Inline JSON values under *.key data keys (e.g. Secret data dumps
		// serialised on a single line).
		name:  "key-material-json",
		re:    regexp.MustCompile(`(?i)("[A-Za-z0-9][A-Za-z0-9_.-]*\.key"[ \t]*:[ \t]*)"[^"<>&]{40,}"`),
		templ: `${1}"` + RedactPlaceholder + `"`,
	},
	{
		// Base64 values under *.key data keys (Secret data such as tls.key),
		// quoted or unquoted.
		name:  "key-material-base64",
		re:    regexp.MustCompile(`(?im)^([ \t]*"?[A-Za-z0-9][A-Za-z0-9_.-]*\.key"?[ \t]*:[ \t]*)"?[A-Za-z0-9+/]{40,}={0,2}"?[ \t]*$`),
		templ: "${1}" + RedactPlaceholder,
	},
	{
		// key=value / key: value occurrences in free text (log lines, captured
		// output, prose) where no line anchoring applies. The value classes
		// stop at markup and entity characters so embedded XML/HTML survives.
		name:  "sensitive-inline-value",
		re:    regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])(` + sensitiveKeyPattern + `)([ \t]*[:=][ \t]*)("[^"<>&]{2,}"|[^\s,;)<>"'&\\]{2,})`),
		templ: "${1}${2}${3}" + RedactPlaceholder,
	},
	{
		// XML-attribute-escaped sensitive key-value content (Go's encoding/xml
		// escapes quotes as &#34;): the shape credentials take inside JUnit
		// failure-message attributes. The redacted value keeps the same
		// entity-encoded quotes so the surrounding XML stays well formed.
		name:  "sensitive-xml-escaped-value",
		re:    regexp.MustCompile(`(?i)((?:&#34;|&quot;|")` + sensitiveKeyPattern + `(?:&#34;|&quot;|")[ \t]*[:=][ \t]*)((?:&#34;|&quot;|"))([^&<>"\s]{2,})((?:&#34;|&quot;|"))`),
		templ: "${1}${2}" + RedactPlaceholder + "${4}",
	},
}

// base64RunRe finds candidate base64 payloads long enough to carry key material.
var base64RunRe = regexp.MustCompile(`[A-Za-z0-9+/]{60,}={0,2}`)

// redactBase64KeyMaterial replaces base64 runs that decode to PEM private-key
// material. This catches key material embedded in free text (captured output,
// logs) where no key name precedes it, without touching public certificate
// payloads.
func redactBase64KeyMaterial(s string) (string, int) {
	count := 0
	out := base64RunRe.ReplaceAllStringFunc(s, func(run string) string {
		der, err := base64.StdEncoding.DecodeString(run)
		if err != nil {
			return run
		}
		if strings.Contains(string(der), "PRIVATE KEY") {
			count++
			return RedactPlaceholder
		}
		return run
	})
	return out, count
}

// Redact removes potentially sensitive content from text destined for the
// console or a diagnostic artifact. Structural exclusion comes first (the
// collector never reads Secrets, kubeconfigs or key files); this scrubber is
// the second line of defence for anything that reaches collected text.
func Redact(s string) string {
	r, _ := RedactCount(s)
	return r
}

// RedactCount redacts s and reports how many substitutions were made. The
// count is recorded in diagnostic bundle summaries as redaction evidence.
func RedactCount(s string) (string, int) {
	total := 0
	for _, r := range redactors {
		matches := r.re.FindAllStringIndex(s, -1)
		if len(matches) == 0 {
			continue
		}
		total += len(matches)
		s = r.re.ReplaceAllString(s, r.templ)
	}
	s, n := redactBase64KeyMaterial(s)
	return s, total + n
}

// redactError text keeps error messages identifiable while scrubbing any
// embedded credential.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	s, _ := RedactCount(err.Error())
	return strings.TrimSpace(s)
}
