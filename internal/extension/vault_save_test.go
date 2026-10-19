package extension

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// fakeVaultSink records what vault.save hands it and answers as told.
type fakeVaultSink struct {
	mu  sync.Mutex
	got []VaultFile
}

func (f *fakeVaultSink) SaveToVault(_ context.Context, v VaultFile) (VaultSaved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, v)
	return VaultSaved{ID: "img-007", Created: true, Name: v.Name, MIME: v.MIME, SizeBytes: len(v.Data)}, nil
}

func (f *fakeVaultSink) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

// pngBytes is a PNG signature padded to n bytes: what http.DetectContentType
// needs to call it image/png.
func pngBytes(n int) []byte {
	b := make([]byte, n)
	copy(b, "\x89PNG\r\n\x1a\n")
	return b
}

func vaultParams(over map[string]any) map[string]any {
	p := map[string]any{
		"client_id": "v-0001", "profile": "p-work", "name": "chart.png", "mime": "image/png",
		"data": base64.StdEncoding.EncodeToString(pngBytes(64)), "source_url": "https://example.test/chart.png",
	}
	for k, v := range over {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return p
}

// callVaultSave runs vault.save's handler as the dispatcher does, for a paired
// browser (serveRequest stamps Origin with the connection's ConnInfo).
func callVaultSave(t *testing.T, sink *fakeVaultSink, params map[string]any) (any, error) {
	t.Helper()
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	srv.SetVaultSink(sink)
	h, ok := srv.handlerFor(MethodVaultSave)
	if !ok {
		t.Fatalf("%s is not registered", MethodVaultSave)
	}
	req := &Request{Method: MethodVaultSave, Params: params, Origin: ConnInfo{Instance: "b1", ConnectedAt: time.Now()}}
	return h(context.Background(), req, func(string, string) {})
}

// refusedInvalid asserts the call was refused as invalid_input before the sink.
func refusedInvalid(t *testing.T, what string, sink *fakeVaultSink, params map[string]any) {
	t.Helper()
	before := sink.calls()
	_, err := callVaultSave(t, sink, params)
	if err == nil || codeOf(t, err) != CodeInvalidInput {
		t.Errorf("%s: err %v, want invalid_input", what, err)
	}
	if sink.calls() != before {
		t.Errorf("%s reached the sink", what)
	}
}

func TestVaultSave_AdvertisedOnlyWithASink(t *testing.T) {
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	if hasMethod(srv.RequestMethods(), MethodVaultSave) {
		t.Fatal("vault.save advertised with no sink")
	}
	srv.SetVaultSink(&fakeVaultSink{})
	if !hasMethod(srv.RequestMethods(), MethodVaultSave) {
		t.Fatal("vault.save not advertised after SetVaultSink")
	}
	srv.SetVaultSink(nil)
	if hasMethod(srv.RequestMethods(), MethodVaultSave) {
		t.Fatal("vault.save still advertised after the sink was removed")
	}
}

func TestVaultSave_HandsTheSinkACheckedFile(t *testing.T) {
	sink := &fakeVaultSink{}
	if _, err := callVaultSave(t, sink, vaultParams(map[string]any{
		"mime": "Image/PNG; charset=binary", "source_url": "https://me:pw@example.test/a/chart.png?x=1#frag",
	})); err != nil {
		t.Fatal(err)
	}
	f := sink.got[0]
	if f.ProfileID != "p-work" || f.ClientID != "v-0001" || f.Name != "chart.png" || f.MIME != "image/png" || f.Ext != ".png" ||
		!bytes.Equal(f.Data, pngBytes(64)) || f.SourceURL != "https://example.test/a/chart.png?x=1" {
		t.Errorf("sink got %+v", f)
	}
}

// The cap is on the decoded bytes: exactly VaultSaveMaxBytes passes, one more
// is refused before the sink.
func TestVaultSave_SizeCap(t *testing.T) {
	sink := &fakeVaultSink{}
	if _, err := callVaultSave(t, sink, vaultParams(map[string]any{"data": base64.StdEncoding.EncodeToString(pngBytes(VaultSaveMaxBytes))})); err != nil {
		t.Fatalf("a file of exactly the cap: %v", err)
	}
	refusedInvalid(t, "a file one byte over the cap", sink, vaultParams(map[string]any{"data": base64.StdEncoding.EncodeToString(pngBytes(VaultSaveMaxBytes + 1))}))
	refusedInvalid(t, "an empty file", sink, vaultParams(map[string]any{"data": ""}))
	refusedInvalid(t, "data that is not base64", sink, vaultParams(map[string]any{"data": "%%%not base64%%%"}))
}

// Only the listed types, and the bytes must be what the type says.
func TestVaultSave_MimeList(t *testing.T) {
	html := base64.StdEncoding.EncodeToString([]byte("<html><script>alert(1)</script></html>"))
	// Each with bytes that sniff as that very type, so only the list refuses it.
	for m, data := range map[string][]byte{
		"application/zip":          []byte("PK\x03\x04\x14\x00\x00\x00"),
		"application/octet-stream": {0x00, 0x01, 0x02, 0x03, 0xfe, 0xff},
		"image/bmp":                append([]byte("BM"), make([]byte, 32)...),
		"image/x-icon":             append([]byte("\x00\x00\x01\x00"), make([]byte, 32)...),
		"text/html":                []byte("<html><body>hi</body></html>"),
		"":                         pngBytes(64),
	} {
		refusedInvalid(t, "type "+m, &fakeVaultSink{}, vaultParams(map[string]any{"mime": m, "data": base64.StdEncoding.EncodeToString(data)}))
	}
	refusedInvalid(t, "HTML sent as image/png", &fakeVaultSink{}, vaultParams(map[string]any{"data": html}))
	refusedInvalid(t, "HTML sent as text/plain", &fakeVaultSink{}, vaultParams(map[string]any{"mime": "text/plain", "name": "a.txt", "data": html}))
	refusedInvalid(t, "a PNG sent as application/pdf", &fakeVaultSink{}, vaultParams(map[string]any{"mime": "application/pdf"}))
	refusedInvalid(t, "text that is not UTF-8", &fakeVaultSink{}, vaultParams(map[string]any{"mime": "text/csv", "name": "a.csv",
		"data": base64.StdEncoding.EncodeToString([]byte("a,b\n\xff\xfe,c\n"))}))

	ok := map[string][]byte{
		"application/pdf": []byte("%PDF-1.7\n1 0 obj\n"),
		"text/markdown":   []byte("# Notes\n\n- one\n"),
		"text/csv":        []byte("name,amount\nAda,3\n"),
		"image/jpeg":      append([]byte("\xff\xd8\xff\xe0"), make([]byte, 32)...),
		"image/gif":       append([]byte("GIF89a"), make([]byte, 32)...),
		"image/webp":      append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...),
	}
	for m, data := range ok {
		sink := &fakeVaultSink{}
		if _, err := callVaultSave(t, sink, vaultParams(map[string]any{"mime": m, "data": base64.StdEncoding.EncodeToString(data)})); err != nil {
			t.Errorf("%s: %v", m, err)
		} else if sink.got[0].Ext != vaultSaveTypes[m] {
			t.Errorf("%s stored as %q", m, sink.got[0].Ext)
		}
	}
}

// A name that could name a folder or climb out of one is refused, not repaired.
func TestVaultSave_NameTraversal(t *testing.T) {
	for _, name := range []string{
		"../../etc/passwd", "a/b.png", `..\..\x.png`, `C:\x.png`, "..", ".", "", "   ",
		"a\x00b.png", "line\nbreak.png", "\u202Egnp.exe", strings.Repeat("a", vaultSaveMaxName+1),
	} {
		refusedInvalid(t, "name "+strings.ToValidUTF8(name, "?"), &fakeVaultSink{}, vaultParams(map[string]any{"name": name}))
	}
	refusedInvalid(t, "a name that is not a string", &fakeVaultSink{}, vaultParams(map[string]any{"name": 42.0}))
	for _, name := range []string{"invoice (1).png", "фото.png", "..hidden.png", strings.Repeat("a", vaultSaveMaxName)} {
		if _, err := callVaultSave(t, &fakeVaultSink{}, vaultParams(map[string]any{"name": name})); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
}

// A request names a profile id that could be a folder, and a client id.
func TestVaultSave_ProfileAndClientID(t *testing.T) {
	for _, p := range []any{nil, "", "../p-work", `p\work`, "_machine"} {
		refusedInvalid(t, "profile "+strings.TrimSpace(func() string { s, _ := p.(string); return s }()), &fakeVaultSink{}, vaultParams(map[string]any{"profile": p}))
	}
	for _, c := range []any{nil, "", "v 1", "v/1", "v\u00e91", strings.Repeat("v", vaultSaveMaxClientID+1)} {
		refusedInvalid(t, "client id", &fakeVaultSink{}, vaultParams(map[string]any{"client_id": c}))
	}
}

// A handler call that no paired socket made is refused: serveRequest stamps
// every request with its connection, and only an authenticated socket gets one.
func TestVaultSave_OnlyFromAPairedBrowser(t *testing.T) {
	sink := &fakeVaultSink{}
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	srv.SetVaultSink(sink)
	h, _ := srv.handlerFor(MethodVaultSave)
	_, err := h(context.Background(), &Request{Method: MethodVaultSave, Params: vaultParams(nil)}, func(string, string) {})
	if err == nil || codeOf(t, err) != CodeInvalidInput || sink.calls() != 0 {
		t.Errorf("a request without a connection: %v, sink calls %d", err, sink.calls())
	}
}

func TestVaultSave_SourceURLIsCleanedOrDropped(t *testing.T) {
	for raw, want := range map[string]string{
		"https://u:p@example.test/x.pdf#page=2":           "https://example.test/x.pdf",
		"http://example.test/a?b=c":                       "http://example.test/a?b=c",
		"data:image/png;base64,AAAA":                      "",
		"blob:https://example.test/1":                     "",
		"javascript:alert(1)":                             "",
		"file:///etc/passwd":                              "",
		"/relative/x.png":                                 "",
		"https://" + strings.Repeat("a", vaultSaveMaxURL): "",
	} {
		if got := cleanSourceURL(raw); got != want {
			t.Errorf("cleanSourceURL(%.40q) = %q, want %q", raw, got, want)
		}
	}
}

// Over the paired socket the reply names the item and nothing else: no path,
// no bytes, nothing read back from the vault. A socket that never paired gets
// no reply at all, and the sink is never reached.
func TestVaultSave_OverTheSocketWritesOnly(t *testing.T) {
	sink := &fakeVaultSink{}
	srv, ext, _ := startCaptureServerWith(t, func(s *Server) { s.SetVaultSink(sink) })

	ext.ask("req-v1", MethodVaultSave, vaultParams(nil))
	reply := ext.settled()
	if !reply.OK {
		t.Fatalf("vault.save failed: %s (%s)", reply.Error, reply.Code)
	}
	b, _ := json.Marshal(reply.Data)
	var keys map[string]any
	_ = json.Unmarshal(b, &keys)
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	slices.Sort(got)
	if strings.Join(got, ",") != "created,id,mime,name,size_bytes" {
		t.Errorf("reply data keys = %v, want created,id,mime,name,size_bytes only", got)
	}
	for _, m := range srv.RequestMethods() {
		if strings.HasPrefix(m, "vault.") && m != MethodVaultSave {
			t.Errorf("the bridge has another vault method: %s", m)
		}
	}

	addr, _ := srv.Addr()
	bad, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/monoagent", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer bad.Close()
	frame, _ := json.Marshal(Request{Kind: KindRequest, ID: "req-v2", Method: MethodVaultSave, Params: vaultParams(nil)})
	_ = bad.WriteMessage(websocket.TextMessage, frame)
	_ = bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, msg, err := bad.ReadMessage(); err == nil {
		t.Errorf("an unpaired socket got an answer: %s", msg)
	}
	if sink.calls() != 1 {
		t.Errorf("sink calls = %d, want 1 (the paired one)", sink.calls())
	}
}
