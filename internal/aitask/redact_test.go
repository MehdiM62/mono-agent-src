package aitask

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The fake secrets are assembled here so that no key-shaped literal sits in
// the source.
var (
	fakeSK     = "s" + "k-live-abcdefghijklmnop0123"
	fakeGH     = "g" + "hp_abcdefghijklmnopqrstuvwxyz0123"
	fakeAWS    = "A" + "KIAABCDEFGHIJKLMNOP"
	fakeJWT    = "e" + "yJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc_def-ghi"
	fakeSlack  = "x" + "oxb-1234567890-abcdefghij"
	fakeBearer = "abcdefghijklmnop.qrstuv"
	fakeQuery  = "abcdefghijklmnopqrstuvwxyz"
)

func redactJSON(t *testing.T, in string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(RedactResult(v))
	return string(b)
}

// A secret inside prose is removed, wherever it sits in the string.
func TestRedactResultFindsSecretsInsideProse(t *testing.T) {
	for name, tc := range map[string]struct{ in, leaked string }{
		"api keys":        {`{"answer":"your key is ` + fakeSK + ` and also ` + fakeGH + `"}`, "abcdefghijklmnop"},
		"bearer header":   {`{"note":"Authorization: Bearer ` + fakeBearer + `"}`, fakeBearer},
		"access_token":    {`{"url":"https://x.test/cb?access_token=` + fakeQuery + `&a=1"}`, fakeQuery},
		"plain token":     {`{"url":"https://x.test/cb?a=1&token=abcdefghijkl"}`, "abcdefghijkl"},
		"aws key":         {`{"t":"use ` + fakeAWS + ` for s3"}`, fakeAWS},
		"jwt":             {`{"t":"cookie ` + fakeJWT + ` ok"}`, "abc_def-ghi"},
		"slack":           {`{"t":"` + fakeSlack + `"}`, "1234567890"},
		"nested in array": {`{"l":[{"m":"see ` + fakeSK + ` here"}]}`, "abcdefghijklmnop0123"},
	} {
		out := redactJSON(t, tc.in)
		if strings.Contains(out, tc.leaked) {
			t.Errorf("%s: leaked %q: %s", name, tc.leaked, out)
		}
		if !strings.Contains(out, RedactedValue) {
			t.Errorf("%s: nothing redacted: %s", name, out)
		}
	}
}

func TestRedactResultKeepsTheRestOfTheSentence(t *testing.T) {
	out := redactJSON(t, `{"a":"call me at 5 pm, key is `+fakeSK+` thanks"}`)
	if !strings.Contains(out, "call me at 5 pm, key is") || !strings.Contains(out, "thanks") {
		t.Errorf("the prose around the secret was lost: %s", out)
	}
}

// Labels for identity numbers keep no value.
func TestRedactResultLabelsForIdentityNumbers(t *testing.T) {
	for _, label := range []string{"Passport number", "Card no.", "Social security number", "IBAN", "Card number"} {
		out := redactJSON(t, `{"fields":[{"label":"`+label+`","value":"X1234567"}]}`)
		if strings.Contains(out, "X1234567") {
			t.Errorf("label %q kept its value: %s", label, out)
		}
	}
	// A key named like one.
	if out := redactJSON(t, `{"passport":"X1234567","iban":"DE89370400440532013000"}`); strings.Contains(out, "X1234567") || strings.Contains(out, "DE89") {
		t.Errorf("secret-named keys kept their value: %s", out)
	}
}

// A placeholder names a secret; it is what a valid workflow holds. It stays,
// under a secret-named key and inside a header value.
func TestRedactResultNeverRedactsPlaceholders(t *testing.T) {
	in := `{"config":{"api_key":"{{secret:k}}","Authorization":"Bearer {{secret:a_rather_long_name_api}}","token":"REPLACE_ME","url":"https://x.test/?access_token={{secret:tok_long_name_here}}","label":"Password","value":"{{secret:pw}}"}}`
	out := redactJSON(t, in)
	for _, want := range []string{`{{secret:k}}`, `Bearer {{secret:a_rather_long_name_api}}`, `REPLACE_ME`, `access_token={{secret:tok_long_name_here}}`, `{{secret:pw}}`} {
		if !strings.Contains(out, want) {
			t.Errorf("%q was redacted: %s", want, out)
		}
	}
	if strings.Contains(out, RedactedValue) {
		t.Errorf("a placeholder was replaced: %s", out)
	}
}

// Ordinary prose, and numbers that merely sit under a *tokens key, are not secrets.
func TestRedactResultLeavesOrdinaryTextAlone(t *testing.T) {
	in := `{"summary":"The token bucket limits requests; a password policy applies. Keys and passports are discussed in the tokenizer notes. See sk-learn and the bearer of this letter.","config":{"max_tokens":512,"stream":true,"secret_enabled":false},"url":"https://x.test/a?page=2&token=true"}`
	var want any
	_ = json.Unmarshal([]byte(in), &want)
	wantB, _ := json.Marshal(want)
	if got := redactJSON(t, in); got != string(wantB) {
		t.Errorf("ordinary content changed:\n got %s\nwant %s", got, wantB)
	}
}

// RE2 is linear: a near megabyte of hostile near-matches finishes quickly and
// the secret at the end still goes.
func TestRedactResultWorkIsBounded(t *testing.T) {
	big := strings.Repeat("Bearer s"+"k-aaaaaaaaaaaaaaaa?token=ab g"+"hp_ e"+"yJ.", 20000)
	big += " " + fakeSK
	start := time.Now()
	out, _ := RedactResult(map[string]any{"a": big}).(map[string]any)["a"].(string)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("redacting %d bytes took %v", len(big), d)
	}
	if strings.Contains(out, "abcdefghijklmnop0123") {
		t.Error("the secret at the end survived")
	}
}
