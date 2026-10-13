package monomind

import (
	"regexp"
	"strings"
)

// Failure classes of workflow-run memory. A node execution's own error_class
// (when the engine records one) wins; ClassifyRunError derives one from the
// error text otherwise.
const (
	ClassNotSetup           = "not_setup"
	ClassBrowserUnavailable = "browser_unavailable"
	ClassSelectorMissing    = "selector_missing"
	ClassAuth               = "auth"
	ClassRateLimited        = "rate_limited"
	ClassUpstream5xx        = "upstream_5xx"
	ClassTimeout            = "timeout"
	ClassInvalidConfig      = "invalid_config"
	ClassRejectedByHuman    = "rejected_by_human"
	ClassOther              = "other"
)

// failureClassDescriptions are the descriptions of the failure-class KG
// entities; the keys are every class run memory emits.
var failureClassDescriptions = map[string]string{
	ClassNotSetup:           "the workflow needs something that is not set up yet: a connection, credential, login or installed tool",
	ClassBrowserUnavailable: "no browser was reachable: the extension bridge or the browser was not connected",
	ClassSelectorMissing:    "a page element the automation looks for was not found (selector or XPath missing)",
	ClassAuth:               "credentials were rejected, expired or lacked permission (HTTP 401/403)",
	ClassRateLimited:        "the remote service throttled requests (HTTP 429, quota or rate limit)",
	ClassUpstream5xx:        "the remote service failed (HTTP 5xx, bad gateway, service unavailable)",
	ClassTimeout:            "a step did not finish in time (timeout, deadline exceeded)",
	ClassInvalidConfig:      "the workflow or a node is configured wrongly (validation, missing or invalid field)",
	ClassRejectedByHuman:    "a person reviewing the step rejected or declined it",
	ClassOther:              "a failure that matches no known class",
}

// KnownFailureClass reports whether c is one of the classes above.
func KnownFailureClass(c string) bool {
	_, ok := failureClassDescriptions[c]
	return ok
}

// classRules are tried in order; the first match wins. Specific signals
// (a status code, a human decision) come before broad words like "invalid".
var classRules = []struct {
	class string
	re    *regexp.Regexp
}{
	{ClassRejectedByHuman, regexp.MustCompile(`(?i)\b(rejected|declined|denied) by (the )?(user|human|reviewer|approver|operator)|\bhuman[- ]in[- ](the[- ])?loop\b.*\b(reject|declin|den)|\bhil\b.*\b(reject|declin|den)|\bapproval (was )?(rejected|denied|declined)`)},
	{ClassRateLimited, regexp.MustCompile(`(?i)\b429\b|too many requests|rate[ _-]?limit|quota (exceeded|exhausted)|\bslow down\b|throttl`)},
	{ClassAuth, regexp.MustCompile(`(?i)\b40[13]\b|unauthori[sz]ed|forbidden|invalid (api )?key|invalid[_ ]token|token (has )?expired|expired token|permission denied|access denied|authentication (failed|required)|not authenticated|invalid credentials`)},
	{ClassUpstream5xx, regexp.MustCompile(`(?i)\b5\d\d\b|bad gateway|service unavailable|internal server error|gateway timeout|upstream (error|failure)`)},
	{ClassTimeout, regexp.MustCompile(`(?i)time[d ]?out|deadline exceeded|took too long`)},
	{ClassBrowserUnavailable, regexp.MustCompile(`(?i)\bbrowser\b.*\b(unavailable|not (connected|running|available|found|reachable)|closed|disconnected)|\bno browser\b|extension (bridge )?(is )?(not connected|unavailable|disconnected|not running)|\bbridge\b.*\b(not connected|unavailable|unreachable|down)|\bcdp\b|target closed|chrome (is )?not (running|found|connected)`)},
	{ClassSelectorMissing, regexp.MustCompile(`(?i)selector|xpath|element (was )?not found|no such element|could not find element|waiting for element|no element match`)},
	{ClassNotSetup, regexp.MustCompile(`(?i)not (set ?up|configured|installed|connected)|no (connection|credential|account)s? (found|configured|set)|missing (connection|credential)|login[_ ]required|needs? (a )?login|run .* (setup|login|connect) first`)},
	{ClassInvalidConfig, regexp.MustCompile(`(?i)invalid (config|configuration|value|input|parameter|field|expression)|validation (error|failed)|required (field|parameter|config)|missing required|is required|unknown node type|unsupported (option|operation|value)|parse (error|failed)|cannot parse|malformed`)},
}

// ClassifyRunError derives a failure class from a node or run error message.
// An empty message is "other".
func ClassifyRunError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ClassOther
	}
	for _, r := range classRules {
		if r.re.MatchString(msg) {
			return r.class
		}
	}
	return ClassOther
}

const redactedText = "<redacted>"

// runErrorRedactions strip credentials from error text before it is stored in
// the knowledge graph. They mirror the redaction the CLI applies to errors
// sent to Jev (cmd/monoagentcli retry_jev.go), which the CLI installs as
// RunMemory.Redact; these are the fallback when nothing is installed.
var runErrorRedactions = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`), "${1}" + redactedText + "@"},
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^\s?#"'<>]*)[?#][^\s"'<>]*`), "${1}?" + redactedText},
	{regexp.MustCompile(`(?i)\b(proxy-)?(authorization)(["']?\s*[:=]\s*["']?)(?:(?:bearer|basic|token|digest|negotiate)\s+)?[^\s"',;})\]]+`), "${1}${2}${3}" + redactedText},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer " + redactedText},
	{regexp.MustCompile(`\b(?:sk|pk|rk)-[A-Za-z0-9_-]{8,}`), redactedText},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`), redactedText},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), redactedText},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), redactedText},
	{regexp.MustCompile(`(?i)\b([\w-]*(?:key|token|secret|passw(?:or)?d|pwd|signature|sig|session|cookie|credential)[\w-]*)(["']?\s*[:=]\s*["']?)[^\s&"',;})\]]+`), "${1}${2}" + redactedText},
}

var longTokenRe = regexp.MustCompile(`[A-Za-z0-9+_=-]{32,}`)

// RedactRunError strips obvious credentials from error text: URL userinfo and
// query strings, Authorization/Bearer values, well-known key prefixes,
// key=/token=/secret= pairs and long opaque runs containing a digit.
func RedactRunError(s string) string {
	for _, r := range runErrorRedactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return longTokenRe.ReplaceAllStringFunc(s, func(run string) string {
		if strings.ContainsAny(run, "0123456789") {
			return redactedText
		}
		return run
	})
}

// shortError collapses whitespace and caps s at n runes.
func shortError(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		s = string(rs[:n-1]) + "…"
	}
	return s
}
