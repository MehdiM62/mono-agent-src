package httpnodes

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type obj = map[string]interface{}

func TestParseCurlRealWorld(t *testing.T) {
	cases := []struct {
		name  string
		cmd   string
		want  obj
		notes int
	}{
		{
			name: "stripe create customer (basic key, form, continuations)",
			cmd: `curl https://api.stripe.com/v1/customers \
  -u sk_test_4eC39HqLyjWDarjtT1zdp7dc: \
  -d name="Jenny Rosen" \
  --data-urlencode email="jenny.rosen@example.com"`,
			want: obj{"method": "POST", "url": "https://api.stripe.com/v1/customers",
				"auth_type": "basic", "auth_username": "sk_test_4eC39HqLyjWDarjtT1zdp7dc", "auth_password": "",
				"body_type": "form", "body": obj{"name": "Jenny Rosen", "email": "jenny.rosen@example.com"}},
		},
		{
			name: "stripe list with -G query data",
			cmd:  `curl -G https://api.stripe.com/v1/charges -u sk_test_x: -d limit=3 -d "expand[]=data.customer"`,
			want: obj{"method": "GET", "url": "https://api.stripe.com/v1/charges", "auth_type": "basic", "auth_username": "sk_test_x", "auth_password": "",
				"query_params": obj{"limit": "3", "expand[]": "data.customer"}},
		},
		{
			name: "github create issue",
			cmd: `curl -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer <YOUR-TOKEN>" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/repos/OWNER/REPO/issues \
  -d '{"title":"Found a bug","body":"I'"'"'m having a problem.","labels":["bug"]}'`,
			want: obj{"method": "POST", "url": "https://api.github.com/repos/OWNER/REPO/issues",
				"headers":   obj{"Accept": "application/vnd.github+json", "Authorization": "Bearer <YOUR-TOKEN>", "X-GitHub-Api-Version": "2022-11-28"},
				"body_type": "json", "body": `{"title":"Found a bug","body":"I'm having a problem.","labels":["bug"]}`},
			notes: 1,
		},
		{
			name: "github get with bundled -sSL",
			cmd:  `curl -sSL -H "Authorization: token ghp_abc" "https://api.github.com/user/repos?per_page=100&page=2"`,
			want: obj{"method": "GET", "url": "https://api.github.com/user/repos?per_page=100&page=2", "headers": obj{"Authorization": "token ghp_abc"}},
		},
		{
			name: "slack chat.postMessage json",
			cmd:  `curl -X POST -H 'Authorization: Bearer xoxb-1234' -H 'Content-type: application/json' --data '{"channel":"C1234567890","text":"Hello world :tada:"}' https://slack.com/api/chat.postMessage`,
			want: obj{"method": "POST", "url": "https://slack.com/api/chat.postMessage", "headers": obj{"Authorization": "Bearer xoxb-1234"},
				"body_type": "json", "body": `{"channel":"C1234567890","text":"Hello world :tada:"}`},
		},
		{
			name: "slack incoming webhook",
			cmd:  `curl -X POST -H 'Content-type: application/json' --data '{"text":"Hello, World!"}' https://hooks.slack.com/services/T000/B000/XXXX`,
			want: obj{"method": "POST", "url": "https://hooks.slack.com/services/T000/B000/XXXX", "body_type": "json", "body": `{"text":"Hello, World!"}`},
		},
		{
			name: "slack files upload multipart",
			cmd:  `curl -F file=@dramacat.gif -F "initial_comment=Shakes the cat" -F channels=C024BE91L,D032AC32T -H "Authorization: Bearer xoxb-1" https://slack.com/api/files.upload`,
			want: obj{"method": "POST", "url": "https://slack.com/api/files.upload", "headers": obj{"Authorization": "Bearer xoxb-1"},
				"body_type": "multipart", "body": obj{"file": obj{"file": "dramacat.gif"}, "initial_comment": "Shakes the cat", "channels": "C024BE91L,D032AC32T"}},
		},
		{
			name: "openai chat completions",
			cmd: `curl https://api.openai.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`,
			want: obj{"method": "POST", "url": "https://api.openai.com/v1/chat/completions", "headers": obj{"Authorization": "Bearer $OPENAI_API_KEY"},
				"body_type": "json", "body": `{    "model": "gpt-4o",    "messages": [{"role": "user", "content": "Hello!"}]  }`},
		},
		{
			name: "openai audio transcription multipart with type",
			cmd:  `curl https://api.openai.com/v1/audio/transcriptions -H "Authorization: Bearer sk-x" -H "Content-Type: multipart/form-data" -F file="@/path/to/file/audio.mp3;type=audio/mpeg" -F model="whisper-1"`,
			want: obj{"method": "POST", "url": "https://api.openai.com/v1/audio/transcriptions", "headers": obj{"Authorization": "Bearer sk-x"},
				"body_type": "multipart", "body": obj{"file": obj{"file": "/path/to/file/audio.mp3", "content_type": "audio/mpeg"}, "model": "whisper-1"}},
		},
		{
			name: "anthropic-style messages with --json",
			cmd:  `curl https://api.example-llm.com/v1/messages --header "x-api-key: k1" --header "anthropic-version: 2023-06-01" --json '{"max_tokens":64,"messages":[{"role":"user","content":"Hi"}]}'`,
			want: obj{"method": "POST", "url": "https://api.example-llm.com/v1/messages", "headers": obj{"x-api-key": "k1", "anthropic-version": "2023-06-01", "Accept": "application/json"},
				"body_type": "json", "body": `{"max_tokens":64,"messages":[{"role":"user","content":"Hi"}]}`},
		},
		{
			name: "chrome copy as cURL (bash) with $'...' and cookies",
			cmd: `curl 'https://example.com/api/items?id=5' \
  -H 'accept: application/json, text/plain, */*' \
  -H 'cookie: session=abc; theme=dark' \
  -H 'user-agent: Mozilla/5.0' \
  --data-raw $'{"note":"it\'s \\u00e9t\u00e9"}' \
  --compressed`,
			want: obj{"method": "POST", "url": "https://example.com/api/items?id=5",
				"headers":   obj{"accept": "application/json, text/plain, */*", "cookie": "session=abc; theme=dark", "user-agent": "Mozilla/5.0"},
				"body_type": "json", "body": `{"note":"it's \u00e9t` + "\u00e9" + `"}`},
			notes: 1,
		},
		{
			name: "attached -XPUT and --data=",
			cmd:  `curl -XPUT --data='{"a":1}' -H"Content-Type: application/json" http://localhost:9200/index/_doc/1`,
			want: obj{"method": "PUT", "url": "http://localhost:9200/index/_doc/1", "body_type": "json", "body": `{"a":1}`},
		},
		{
			name: "HEAD with -I and insecure",
			cmd:  `curl -k -I https://self-signed.example.org/health`,
			want: obj{"method": "HEAD", "url": "https://self-signed.example.org/health"}, notes: 1,
		},
		{
			name: "xml raw body with timeout, proxy and user agent",
			cmd:  `curl --request POST --url https://soap.example.com/svc --header 'Content-Type: text/xml; charset=utf-8' --data '<Envelope/>' -m 15 -x http://proxy:3128 -A monoagent/1`,
			want: obj{"method": "POST", "url": "https://soap.example.com/svc", "headers": obj{"User-Agent": "monoagent/1"},
				"body_type": "raw", "raw_content_type": "text/xml; charset=utf-8", "body": "<Envelope/>", "timeout": 15.0, "proxy": "http://proxy:3128"},
		},
		{
			name: "twilio send sms form with repeated --data-urlencode",
			cmd: `curl -X POST "https://api.twilio.com/2010-04-01/Accounts/AC123/Messages.json" \
--data-urlencode "Body=Hi there & welcome" \
--data-urlencode "From=+15017122661" \
--data-urlencode "To=+15558675310" \
-u AC123:authtok`,
			want: obj{"method": "POST", "url": "https://api.twilio.com/2010-04-01/Accounts/AC123/Messages.json",
				"auth_type": "basic", "auth_username": "AC123", "auth_password": "authtok",
				"body_type": "form", "body": obj{"Body": "Hi there & welcome", "From": "+15017122661", "To": "+15558675310"}},
		},
		{
			name: "oauth2 bearer and repeated form keys stay encoded",
			cmd:  `curl --oauth2-bearer tok123 -d tag=a -d tag=b https://api.example.com/tags`,
			want: obj{"method": "POST", "url": "https://api.example.com/tags", "auth_type": "bearer", "auth_token": "tok123", "body_type": "form", "body": "tag=a&tag=b"},
		},
		{
			name: "windows cmd caret continuation and no scheme",
			cmd:  "curl api.example.com/ping ^\n  -H \"X-A: 1\"",
			want: obj{"method": "GET", "url": "http://api.example.com/ping", "headers": obj{"X-A": "1"}},
		},
		{
			name: "double-quoted escapes",
			cmd:  `curl -X PATCH "https://api.example.com/u/1" -H "Content-Type: application/json" -d "{\"name\":\"O\\\"Neil\",\"cost\":\"\$5\"}"`,
			want: obj{"method": "PATCH", "url": "https://api.example.com/u/1", "body_type": "json", "body": `{"name":"O\"Neil","cost":"$5"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseCurl(tc.cmd)
			if err != nil {
				t.Fatalf("ParseCurl: %v", err)
			}
			got := normalize(t, req.NodeConfig())
			want := normalize(t, tc.want)
			if !reflect.DeepEqual(got, want) {
				gb, _ := json.MarshalIndent(got, "", " ")
				wb, _ := json.MarshalIndent(want, "", " ")
				t.Fatalf("config\n%s\nwant\n%s", gb, wb)
			}
			if len(req.Notes) != tc.notes {
				t.Fatalf("notes = %q, want %d", req.Notes, tc.notes)
			}
		})
	}
}

func normalize(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func TestParseCurlErrors(t *testing.T) {
	for _, cmd := range []string{
		"curl",
		"curl -H 'X: 1'",
		"curl 'https://x.com",
		`curl "https://x.com`,
		"curl https://x.com -H",
		"curl https://x.com -F novalue",
	} {
		if _, err := ParseCurl(cmd); err == nil {
			t.Errorf("%q: no error", cmd)
		}
	}
}

func TestParseCurlNotes(t *testing.T) {
	req, err := ParseCurl(`curl -o out.json -d @payload.json --retry 3 https://a.example https://b.example`)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(req.Notes, "|")
	for _, want := range []string{"-o ignored", "payload.json", "--retry ignored", "extra URL"} {
		if !strings.Contains(strings.ReplaceAll(joined, "option ", ""), want) {
			t.Errorf("notes %q lack %q", joined, want)
		}
	}
	if req.URL != "https://a.example" {
		t.Fatalf("URL = %q", req.URL)
	}
}

// A parsed config runs: the request the node sends matches the command.
func TestParseCurlConfigRuns(t *testing.T) {
	rec := newRecorder(t, nil)
	req, err := ParseCurl(`curl -X POST ` + rec.URL + `/v1/x -u ann:pw -H 'X-Trace: 9' -d a=1 -d b=two`)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, &RequestNode{}, req.NodeConfig())
	got := rec.last(t)
	if got.Method != "POST" || got.Path != "/v1/x" || got.Body != "a=1&b=two" || got.Header.Get("X-Trace") != "9" ||
		got.Header.Get("Authorization") != basicHeader("ann", "pw") || got.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("request: %+v", got)
	}
}
