package autostart

import "testing"

func TestProfileFromDefinition(t *testing.T) {
	cases := map[string]struct {
		text string
		want string
		ok   bool
	}{
		"systemd":  {"ExecStart=\"/bin/x\" daemon --profile \"p-1\"\nRestart=on-failure", "p-1", true},
		"plist":    {"<string>daemon</string>\n<string>--profile</string>\n<string>p-2</string>", "p-2", true},
		"schtasks": {"<Arguments>/c &quot;&quot;C:\\x.exe&quot; daemon --profile &quot;p-3&quot; &gt;&gt; log&quot;</Arguments>", "p-3", true},
		"equals":   {"ExecStart=/bin/x daemon --profile=p-4", "p-4", true},
		"old unit": {"ExecStart=\"/bin/x\" daemon\n", "", false},
		"dangling": {"ExecStart=/bin/x daemon --profile", "", false},
	}
	for name, c := range cases {
		got, ok := profileFromDefinition(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q, %v want %q, %v", name, got, ok, c.want, c.ok)
		}
	}
}
