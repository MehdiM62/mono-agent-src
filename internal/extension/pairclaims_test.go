package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/account/accounttest"
)

type fakeCards struct {
	mu      sync.Mutex
	opened  []PendingClaim
	settled map[string]string
}

func (f *fakeCards) Open(_ context.Context, c PendingClaim) {
	f.mu.Lock()
	f.opened = append(f.opened, c)
	f.mu.Unlock()
}

func (f *fakeCards) Settle(_ context.Context, id, status string) {
	f.mu.Lock()
	if f.settled == nil {
		f.settled = map[string]string{}
	}
	f.settled[id] = status
	f.mu.Unlock()
}

func poll(srv *Server, id, secret string, mut func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9222/monoagent/pair/claim/"+id, nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Origin", extOrigin)
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	if mut != nil {
		mut(r)
	}
	w := httptest.NewRecorder()
	srv.handlePairPoll(w, r)
	return w
}

func askClaim(t *testing.T, srv *Server, i int, origin string) claimReply {
	t.Helper()
	w := claim(srv, instanceReq(i, origin))
	var rep claimReply
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if w.Code != http.StatusAccepted || !rep.Pending || rep.ClaimID == "" || len(rep.PollSecret) != 64 {
		t.Fatalf("ask-mode claim = %d %s", w.Code, w.Body)
	}
	return rep
}

// The headline: a claim with a spoofed Origin gets no token until the owner
// approves it, whatever the caller does.
func TestAskModeSpoofedOriginGetsNoTokenWithoutApproval(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	cards := &fakeCards{}
	srv.SetPairCards(cards)
	w := claim(srv, claimReq(nil))
	if strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("the claim reply carries a token: %s", w.Body)
	}
	var rep claimReply
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if w.Code != http.StatusAccepted || !rep.Pending {
		t.Fatalf("claim = %d %s", w.Code, w.Body)
	}
	srv.flushCards()
	for i := 0; i < 3; i++ {
		p := poll(srv, rep.ClaimID, rep.PollSecret, nil)
		if p.Code != 200 || strings.Contains(p.Body.String(), `"token"`) || !strings.Contains(p.Body.String(), `"pending"`) {
			t.Fatalf("poll %d before approval = %d %s", i, p.Code, p.Body)
		}
	}
	if n := pairedBrowserCount(); n != 0 {
		t.Fatalf("%d browsers paired without approval", n)
	}
	if len(cards.opened) != 1 || cards.opened[0].ID != rep.ClaimID || !strings.Contains(cards.opened[0].Origin, "chrome-extension://") {
		t.Fatalf("cards opened = %+v", cards.opened)
	}
}

func TestAskModeApprovalYieldsExactlyOneToken(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	cards := &fakeCards{}
	srv.SetPairCards(cards)
	rep := askClaim(t, srv, 1, extOrigin)
	if _, err := DecidePendingClaim(rep.ClaimID, true); err != nil {
		t.Fatal(err)
	}
	p := poll(srv, rep.ClaimID, rep.PollSecret, nil)
	var got claimReply
	_ = json.Unmarshal(p.Body.Bytes(), &got)
	if p.Code != 200 || got.Status != ClaimApproved || len(got.Token) != 64 {
		t.Fatalf("poll after approval = %d %s", p.Code, p.Body)
	}
	if _, ok := pairedBrowserForToken(got.Token); !ok {
		t.Fatal("the delivered token does not work")
	}
	if again := poll(srv, rep.ClaimID, rep.PollSecret, nil); again.Code != 404 || strings.Contains(again.Body.String(), `"token"`) {
		t.Fatalf("a second poll = %d %s: the token must be delivered once", again.Code, again.Body)
	}
	if n := pairedBrowserCount(); n != 1 {
		t.Fatalf("%d browsers after one approval, want 1", n)
	}
	srv.flushCards()
	if cards.settled[rep.ClaimID] != "approved" {
		t.Fatalf("card status = %q", cards.settled[rep.ClaimID])
	}
	if left, _ := ListPendingClaims(); len(left) != 0 {
		t.Fatalf("pending record not destroyed: %+v", left)
	}
}

func TestAskModeDenyAndExpire(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	cards := &fakeCards{}
	srv.SetPairCards(cards)

	d := askClaim(t, srv, 1, extOrigin)
	if _, err := DecidePendingClaim(d.ClaimID, false); err != nil {
		t.Fatal(err)
	}
	p := poll(srv, d.ClaimID, d.PollSecret, nil)
	if p.Code != 200 || !strings.Contains(p.Body.String(), `"denied"`) || strings.Contains(p.Body.String(), `"token"`) {
		t.Fatalf("poll after deny = %d %s", p.Code, p.Body)
	}
	srv.flushCards()
	if cards.settled[d.ClaimID] != "rejected" {
		t.Fatalf("denied card = %q", cards.settled[d.ClaimID])
	}

	e := askClaim(t, srv, 2, extOrigin)
	expireClaimNow(t, e.ClaimID)
	if _, err := DecidePendingClaim(e.ClaimID, true); err == nil {
		t.Fatal("an expired claim could be approved")
	}
	p = poll(srv, e.ClaimID, e.PollSecret, nil)
	if p.Code != http.StatusGone || strings.Contains(p.Body.String(), `"token"`) {
		t.Fatalf("poll after expiry = %d %s", p.Code, p.Body)
	}
	if n := pairedBrowserCount(); n != 0 {
		t.Fatalf("%d browsers paired from denied/expired claims", n)
	}

	// An approved-then-expired claim yields nothing either.
	f := askClaim(t, srv, 3, extOrigin)
	if _, err := DecidePendingClaim(f.ClaimID, true); err != nil {
		t.Fatal(err)
	}
	expireClaimNow(t, f.ClaimID)
	if p := poll(srv, f.ClaimID, f.PollSecret, nil); p.Code != http.StatusGone {
		t.Fatalf("poll of an approved but expired claim = %d %s", p.Code, p.Body)
	}
	if n := pairedBrowserCount(); n != 0 {
		t.Fatalf("an expired approval minted a token")
	}
}

func expireClaimNow(t *testing.T, id string) {
	t.Helper()
	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		if list[i].ID == id {
			list[i].ExpiresAt = time.Now().Add(-time.Second)
		}
	}
	if err := writePendingLocked(list); err != nil {
		t.Fatal(err)
	}
}

func TestAskModePollNeedsTheSecretAndTheOrigin(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	rep := askClaim(t, srv, 1, extOrigin)
	if _, err := DecidePendingClaim(rep.ClaimID, true); err != nil {
		t.Fatal(err)
	}
	other := "chrome-extension://otherotherotherotherotherotherot"
	for name, p := range map[string]*httptest.ResponseRecorder{
		"no secret":            poll(srv, rep.ClaimID, "", nil),
		"wrong secret":         poll(srv, rep.ClaimID, strings.Repeat("a", 64), nil),
		"id as secret":         poll(srv, rep.ClaimID, rep.ClaimID, nil),
		"unknown claim":        poll(srv, "0000000000000000", rep.PollSecret, nil),
		"other origin":         poll(srv, rep.ClaimID, rep.PollSecret, func(r *http.Request) { r.Header.Set("Origin", other) }),
		"web origin":           poll(srv, rep.ClaimID, rep.PollSecret, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }),
		"no origin, no secret": poll(srv, rep.ClaimID, "", func(r *http.Request) { r.Header.Del("Origin") }),
		"rebound host":         poll(srv, rep.ClaimID, rep.PollSecret, func(r *http.Request) { r.Host = "evil.example" }),
		"remote peer":          poll(srv, rep.ClaimID, rep.PollSecret, func(r *http.Request) { r.RemoteAddr = "192.0.2.7:1" }),
	} {
		if p.Code != 404 && p.Code != 403 || strings.Contains(p.Body.String(), `"token"`) {
			t.Errorf("%s: poll = %d %s", name, p.Code, p.Body)
		}
	}
	if n := pairedBrowserCount(); n != 0 {
		t.Fatalf("a poll without the secret minted a token")
	}
	// Chrome sends no Origin on a GET: the secret is the proof.
	noOrigin := func(r *http.Request) { r.Header.Del("Origin") }
	if p := poll(srv, rep.ClaimID, rep.PollSecret, noOrigin); p.Code != 200 || !strings.Contains(p.Body.String(), `"token"`) {
		t.Fatalf("the right poll after the wrong ones = %d %s", p.Code, p.Body)
	}
}

func TestAskModeQueueIsBoundedAndRefusesRatherThanEvicts(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	// One origin: its share of the queue, then refused.
	a := askClaim(t, srv, 1, extOrigin)
	askClaim(t, srv, 2, extOrigin)
	if w := claim(srv, instanceReq(3, extOrigin)); w.Code != 429 || strings.Contains(w.Body.String(), "pollSecret") {
		t.Fatalf("third claim of one origin = %d %s", w.Code, w.Body)
	}
	// Other origins fill the queue; the next is refused, and nothing waiting is dropped.
	for i := 0; i < maxPendingClaims-2; i++ {
		askClaim(t, srv, 10+i, "chrome-extension://"+strings.Repeat(string(rune('a'+i)), 32))
	}
	if w := claim(srv, instanceReq(30, "chrome-extension://"+strings.Repeat("z", 32))); w.Code != 429 {
		t.Fatalf("claim into a full queue = %d %s", w.Code, w.Body)
	}
	list, _ := ListPendingClaims()
	if len(list) != maxPendingClaims {
		t.Fatalf("%d pending, want %d", len(list), maxPendingClaims)
	}
	if p := poll(srv, a.ClaimID, a.PollSecret, nil); p.Code != 200 {
		t.Fatalf("the oldest waiting claim was evicted: %d %s", p.Code, p.Body)
	}
}

// Approving a claim for an instance that is paired replaces it: that is the
// owner's decision, shown on the card; nothing else replaces it.
func TestAskModeApprovalMayReplaceAnInstanceTheOwnerSees(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	tok, _, err := addPairedBrowser(PairedBrowser{Label: "old", Instance: "0123456789abcdef"}, mintOpts{})
	if err != nil {
		t.Fatal(err)
	}
	rep := askClaim(t, srv, 0, extOrigin)
	_ = rep
	w := claim(srv, claimReq(nil)) // same instance as the paired one
	var r2 claimReply
	_ = json.Unmarshal(w.Body.Bytes(), &r2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("claim = %d %s", w.Code, w.Body)
	}
	if _, ok := pairedBrowserForToken(tok); !ok {
		t.Fatal("a pending claim killed the existing token before the owner decided")
	}
	_, _ = DecidePendingClaim(r2.ClaimID, true)
	p := poll(srv, r2.ClaimID, r2.PollSecret, nil)
	if p.Code != 200 || !strings.Contains(p.Body.String(), `"token"`) {
		t.Fatalf("poll = %d %s", p.Code, p.Body)
	}
	if _, ok := pairedBrowserForToken(tok); ok {
		t.Fatal("the approved claim did not replace the old token of its instance")
	}
}

func TestDecidePendingClaimLatestAndPrefix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _, _ := CreatePendingClaim(PendingClaim{Label: "a", Origin: "chrome-extension://a"})
	time.Sleep(5 * time.Millisecond)
	b, _, _ := CreatePendingClaim(PendingClaim{Label: "b", Origin: "chrome-extension://b"})
	if _, err := DecidePendingClaim("ab", true); err == nil {
		t.Fatal("a 2-character prefix must be refused")
	}
	got, err := DecidePendingClaim("", true)
	if err != nil || got.ID != b.ID {
		t.Fatalf("latest = %+v, %v; want %s", got, err, b.ID)
	}
	if got, err = DecidePendingClaim(a.ID[:6], false); err != nil || got.ID != a.ID || got.Status != ClaimDenied {
		t.Fatalf("by prefix = %+v, %v", got, err)
	}
	if _, err := DecidePendingClaim("", true); err == nil {
		t.Fatal("nothing is waiting any more")
	}
}
