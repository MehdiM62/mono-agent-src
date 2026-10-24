package tlsserve

import "testing"

func TestClientAddr(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:9322": "127.0.0.1:9322", ":9322": "127.0.0.1:9322", "[::]:9322": "[::1]:9322",
		"127.0.0.1:9322": "127.0.0.1:9322", "192.168.1.5:9322": "192.168.1.5:9322", "nas.lan:1": "nas.lan:1", "junk": "junk",
	} {
		if got := ClientAddr(in); got != want {
			t.Errorf("ClientAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
