package profileorder

import "testing"

func TestOlderComparesBothStoredFormatsByTime(t *testing.T) {
	cases := []struct {
		name     string
		ca, ia   string
		cb, ib   string
		aBeforeB bool
	}{
		{"mixed formats, same second: id decides", "2026-10-16 06:04:34", "default", "2026-10-16T06:04:34Z", "work", true},
		{"mixed formats, same second, id reversed", "2026-10-16 06:04:34", "zzz", "2026-10-16T06:04:34Z", "aaa", false},
		{"RFC3339 older than the space format", "2026-10-16T06:04:33Z", "work", "2026-10-16 06:04:34", "default", true},
		{"space format older than RFC3339", "2026-10-16 06:04:33", "x", "2026-10-16T06:04:34Z", "a", true},
		{"unreadable sorts last", "garbage", "a", "2026-10-16 06:04:34", "z", false},
	}
	for _, c := range cases {
		if got := Older(c.ca, c.ia, c.cb, c.ib); got != c.aBeforeB {
			t.Errorf("%s: Older = %v, want %v", c.name, got, c.aBeforeB)
		}
		if c.aBeforeB && Older(c.cb, c.ib, c.ca, c.ia) {
			t.Errorf("%s: order is not antisymmetric", c.name)
		}
	}
}
