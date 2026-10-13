package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// RFC 6238 Appendix B test vectors (8 digits).
func TestTOTPRFC6238Vectors(t *testing.T) {
	secrets := map[string]string{
		"SHA1":   "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"SHA256": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA",
		"SHA512": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNA",
	}
	vectors := []struct {
		at   int64
		want map[string]string
	}{
		{59, map[string]string{"SHA1": "94287082", "SHA256": "46119246", "SHA512": "90693936"}},
		{1111111109, map[string]string{"SHA1": "07081804", "SHA256": "68084774", "SHA512": "25091201"}},
		{20000000000, map[string]string{"SHA1": "65353130", "SHA256": "77737706", "SHA512": "47863826"}},
	}
	for _, v := range vectors {
		for alg, want := range v.want {
			node := &TOTPNode{now: func() time.Time { return time.Unix(v.at, 0) }}
			got := run(t, node, []workflow.Item{{}}, map[string]interface{}{"secret": secrets[alg], "algorithm": alg, "digits": 8.0})
			if got[0]["token"] != want {
				t.Errorf("%s @%d: got %v want %s", alg, v.at, got[0]["token"], want)
			}
		}
	}
	node := &TOTPNode{now: func() time.Time { return time.Unix(65, 0) }}
	got := run(t, node, []workflow.Item{{}, {}}, map[string]interface{}{"secret": "gezd gnbv gy3t qojq gezd gnbv gy3t qojq", "operation": "generateSecret"})
	if len(got) != 2 || got[0]["secondsRemaining"] != 25 || len(got[0]["token"].(string)) != 6 {
		t.Errorf("defaults: %v", got)
	}
	for _, cfg := range []map[string]interface{}{
		{"secret": ""},
		{"secret": "!!!"},
		{"secret": "GEZDGNBV", "label": "nocolon"},
		{"secret": "GEZDGNBV", "algorithm": "MD4"},
		{"secret": "GEZDGNBV", "digits": 12.0},
		{"secret": "GEZDGNBV", "operation": "verify"},
	} {
		if _, err := (&TOTPNode{}).Execute(context.Background(), workflow.NodeInput{}, cfg); err == nil {
			t.Errorf("%v: expected error", cfg)
		}
	}
}

func TestDebugHelper(t *testing.T) {
	node := &DebugHelperNode{}
	in := workflow.NodeInput{Items: []workflow.Item{{JSON: map[string]interface{}{}}}}
	_, err := node.Execute(context.Background(), in, map[string]interface{}{"category": "throwError", "throw_error_message": "boom"})
	if !errors.Is(err, ErrDebugHelper) || err.Error() == "" {
		t.Errorf("throw_error: %v", err)
	}
	out, err := node.Execute(context.Background(), in, map[string]interface{}{"category": "doNothing"})
	if err != nil || len(out[0].Items) != 0 {
		t.Errorf("do_nothing: %v %v", out, err)
	}
	if _, err := node.Execute(context.Background(), in, map[string]interface{}{"category": "oom"}); err == nil {
		t.Error("oom: expected not-supported error")
	}
	wantKeys := map[string][]string{
		"user": {"uid", "email", "firstname", "lastname", "password"}, "email": {"email", "confirmed"},
		"address": {"street", "city", "zip", "state", "country"}, "creditCard": {"type", "number", "ccv", "exp", "holder_name"},
		"uuid": {"uuid"}, "macAddress": {"mac"}, "ipv4": {"ip"}, "ipv6": {"ipv6"}, "latLong": {"location"},
		"semver": {"version"}, "url": {"url"}, "nanoid": {"nanoId"},
	}
	for kind, keys := range wantKeys {
		cfg := map[string]interface{}{"category": "randomData", "random_data_type": kind, "random_data_count": 3.0, "random_data_seed": "s"}
		got := run(t, node, in.Items, cfg)
		if len(got) != 3 {
			t.Fatalf("%s: %d items", kind, len(got))
		}
		for _, k := range keys {
			if _, ok := got[0][k]; !ok {
				t.Errorf("%s: missing %q in %v", kind, k, got[0])
			}
		}
		again := run(t, node, in.Items, cfg)
		if asJSON(again) != asJSON(got) {
			t.Errorf("%s: same seed gave different data", kind)
		}
	}
	got := run(t, node, in.Items, map[string]interface{}{"category": "random_data", "random_data_type": "credit_card", "random_data_count": 50.0, "random_data_single_array": true})
	list := got[0]["generatedItems"].([]interface{})
	if len(got) != 1 || len(list) != 50 {
		t.Fatalf("single array: %v", got)
	}
	for _, c := range list {
		if !luhnValid(c.(map[string]interface{})["number"].(string)) {
			t.Errorf("card number fails Luhn: %v", c)
		}
	}
	got = run(t, node, in.Items, map[string]interface{}{"category": "random_data", "random_data_type": "nanoid", "random_data_count": 1.0, "nanoid_alphabet": "ab", "nanoid_length": 40.0})
	if s := got[0]["nanoId"].(string); len(s) != 40 || len(trimAll(s, "ab")) != 0 {
		t.Errorf("nanoid: %q", s)
	}
	if _, err := node.Execute(context.Background(), in, map[string]interface{}{"category": "random_data", "random_data_type": "pets"}); err == nil {
		t.Error("unknown type: expected error")
	}
}

func luhnValid(n string) bool {
	sum := 0
	for i := len(n) - 1; i >= 0; i-- {
		d := int(n[i] - '0')
		if (len(n)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

func trimAll(s, chars string) string {
	out := []rune{}
	for _, r := range s {
		keep := true
		for _, c := range chars {
			if r == c {
				keep = false
			}
		}
		if keep {
			out = append(out, r)
		}
	}
	return string(out)
}
