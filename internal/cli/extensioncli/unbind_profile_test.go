package extensioncli

import (
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/extension"
)

type fakeBridge struct {
	browsers []extension.ConnInfo
	listErr  error
	failOn   map[string]bool
	unbound  []string
}

func (f *fakeBridge) Browsers() ([]extension.ConnInfo, error) { return f.browsers, f.listErr }
func (f *fakeBridge) SetBinding(instance, profile, label string) error {
	if profile != "" || label != "" {
		return errors.New("only unbinding is expected")
	}
	if f.failOn[instance] {
		return errors.New("browser went away")
	}
	f.unbound = append(f.unbound, instance)
	return nil
}

func TestUnbindProfileUnbindsOnlyThatProfilesBrowsers(t *testing.T) {
	f := &fakeBridge{browsers: []extension.ConnInfo{
		{Instance: "a", Profile: "p1"}, {Instance: "b", Profile: "p2"}, {Instance: "c", Profile: "p1"},
		{Instance: "old", Profile: "p1", Legacy: true}, {Instance: "free"},
	}}
	n, err := unbindProfile(f, "p1")
	if err != nil || n != 2 || strings.Join(f.unbound, ",") != "a,c" {
		t.Fatalf("unbound %v (n=%d, err=%v), want a and c", f.unbound, n, err)
	}
}

// One browser failing must not leave the later ones bound.
func TestUnbindProfileGoesOnAfterAFailureAndReportsEveryOne(t *testing.T) {
	f := &fakeBridge{
		browsers: []extension.ConnInfo{{Instance: "a", Profile: "p1"}, {Instance: "b", Profile: "p1"}, {Instance: "c", Profile: "p1"}, {Instance: "d", Profile: "p1"}},
		failOn:   map[string]bool{"a": true, "c": true},
	}
	n, err := unbindProfile(f, "p1")
	if n != 2 || strings.Join(f.unbound, ",") != "b,d" {
		t.Fatalf("unbound %v (n=%d), want b and d", f.unbound, n)
	}
	if err == nil || !strings.Contains(err.Error(), "unbinding a") || !strings.Contains(err.Error(), "unbinding c") {
		t.Fatalf("err %v, want both failures named", err)
	}
}

func TestUnbindProfileReportsAnUnreachableBridge(t *testing.T) {
	if _, err := unbindProfile(&fakeBridge{listErr: errors.New("refused")}, "p1"); err == nil {
		t.Fatal("a bridge that cannot list its browsers must be reported")
	}
}
