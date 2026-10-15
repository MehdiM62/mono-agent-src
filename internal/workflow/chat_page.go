package workflow

import (
	"crypto/rand"
	"encoding/base64"
	"html/template"
	"net/http"
)

// writeChatPage serves a trigger.chat's page: the messages of the
// browser's session (kept in sessionStorage), a box to type in, and the
// approval buttons a core.respond_chat sends. It polls <path>/messages for
// new messages and posts to <path>/message.
func writeChatPage(w http.ResponseWriter, route *chatRoute) {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	n := base64.RawStdEncoding.EncodeToString(nonce)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+n+"'; connect-src 'self'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	title := route.Title
	if title == "" {
		title = "Chat"
	}
	_ = chatPage.Execute(w, map[string]interface{}{"Title": title, "Subtitle": route.Subtitle,
		"Placeholder": orDefault(route.Placeholder, "Type your question…"), "Initial": route.InitialMessages,
		"Base": "/chat/" + route.Path, "Nonce": n})
}

var chatPage = template.Must(template.New("chat").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;margin:0;background:#f6f6f4;color:#1d1d1b;display:flex;flex-direction:column;height:100vh}
header{padding:1rem 1.25rem;background:#1d1d1b;color:#fff}header h1{font-size:1.15rem;margin:0}header p{margin:.2rem 0 0;opacity:.8}
#log{flex:1;overflow-y:auto;padding:1rem;display:flex;flex-direction:column;gap:.6rem}
.m{max-width:80%;padding:.6rem .85rem;border-radius:12px;white-space:pre-wrap;word-wrap:break-word}
.bot{align-self:flex-start;background:#fff;border:1px solid #ddd}.user{align-self:flex-end;background:#1d4ed8;color:#fff}
.m button{display:inline-block;margin:.5rem .5rem 0 0;padding:.4rem .9rem;font:inherit;border-radius:6px;border:1px solid #1d1d1b;background:#fff;cursor:pointer}
.m button.p{background:#1d1d1b;color:#fff}
form{display:flex;gap:.5rem;padding:.75rem;background:#fff;border-top:1px solid #ddd}
input{flex:1;padding:.6rem;font:inherit;border:1px solid #8a8a85;border-radius:6px}
form button{padding:.6rem 1.1rem;font:inherit;border:0;border-radius:6px;background:#1d4ed8;color:#fff;cursor:pointer}
:focus-visible{outline:3px solid #f59e0b;outline-offset:2px}
@media (prefers-color-scheme:dark){body{background:#151514;color:#eee}.bot{background:#232321;border-color:#333}form{background:#1b1b1a;border-color:#333}input{background:#151514;color:#eee}}
</style></head><body>
<header><h1>{{.Title}}</h1>{{with .Subtitle}}<p>{{.}}</p>{{end}}</header>
<div id="log" role="log" aria-live="polite">{{range .Initial}}<div class="m bot">{{.}}</div>{{end}}</div>
<form id="f"><input id="q" name="q" autocomplete="off" aria-label="Message" placeholder="{{.Placeholder}}" required><button>Send</button></form>
<script nonce="{{.Nonce}}">
(function () {
  var base = {{.Base}}, log = document.getElementById("log"), q = document.getElementById("q");
  var key = "monoagent-chat:" + base, sid = "";
  try { sid = sessionStorage.getItem(key) || ""; } catch (e) {}
  if (!sid) {
    sid = (crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random().toString(16).slice(2));
    try { sessionStorage.setItem(key, sid); } catch (e) {}
  }
  var after = 0;
  function add(m) {
    var d = document.createElement("div");
    d.className = "m " + (m.role === "user" ? "user" : "bot");
    d.textContent = m.text;
    (m.buttons || []).forEach(function (b) {
      var btn = document.createElement("button");
      btn.type = "button"; btn.textContent = b.label;
      if (b.primary) btn.className = "p";
      btn.onclick = function () {
        d.querySelectorAll("button").forEach(function (x) { x.disabled = true; });
        fetch(new URL(b.url).pathname, { method: "POST" }).then(function () { q.disabled = false; });
      };
      d.appendChild(btn);
    });
    q.disabled = !!m.blockInput;
    log.appendChild(d);
    log.scrollTop = log.scrollHeight;
  }
  function poll() {
    fetch(base + "/messages?sessionId=" + encodeURIComponent(sid) + "&after=" + after)
      .then(function (r) { return r.json(); })
      .then(function (j) { (j.messages || []).forEach(function (m) { after = m.id; add(m); }); poll(); })
      .catch(function () { setTimeout(poll, 3000); });
  }
  document.getElementById("f").onsubmit = function (ev) {
    ev.preventDefault();
    var text = q.value.trim();
    if (!text) return;
    q.value = "";
    fetch(base + "/message", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "sendMessage", sessionId: sid, chatInput: text }) });
  };
  poll();
})();
</script>
</body></html>
`))
