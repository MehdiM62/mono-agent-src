package apipkg

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// azureNow is replaceable in tests.
var azureNow = func() time.Time { return time.Now().UTC() }

func azureKey(tmpl string, sc Scope) ([]byte, error) {
	k, err := renderText(tmpl, sc)
	if err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("the key must be base64")
	}
	return b, nil
}

// signAzureSharedKey signs an Azure Storage request (SharedKey scheme).
func signAzureSharedKey(req *http.Request, a *AzureSharedKeyInject, sc Scope) error {
	account, err := renderText(a.Account, sc)
	if err != nil || account == "" {
		return fmt.Errorf("azure_shared_key: account is empty")
	}
	key, err := azureKey(a.Key, sc)
	if err != nil {
		return fmt.Errorf("azure_shared_key: %w", err)
	}
	req.Header.Set("x-ms-date", azureNow().Format(http.TimeFormat))
	h := req.Header
	length := ""
	if req.ContentLength > 0 {
		length = fmt.Sprint(req.ContentLength)
	}
	parts := []string{req.Method, h.Get("Content-Encoding"), h.Get("Content-Language"), length,
		h.Get("Content-MD5"), h.Get("Content-Type"), "", h.Get("If-Modified-Since"), h.Get("If-Match"),
		h.Get("If-None-Match"), h.Get("If-Unmodified-Since"), h.Get("Range")}
	var b strings.Builder
	b.WriteString(strings.Join(parts, "\n") + "\n")
	var ms []string
	for k := range h {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "x-ms-") {
			ms = append(ms, lk)
		}
	}
	sort.Strings(ms)
	for _, k := range ms {
		b.WriteString(k + ":" + strings.TrimSpace(h.Get(k)) + "\n")
	}
	b.WriteString("/" + account + req.URL.EscapedPath())
	q := req.URL.Query()
	names := make([]string, 0, len(q))
	low := map[string][]string{}
	for k, v := range q {
		lk := strings.ToLower(k)
		if _, ok := low[lk]; !ok {
			names = append(names, lk)
		}
		low[lk] = append(low[lk], v...)
	}
	sort.Strings(names)
	for _, k := range names {
		vals := low[k]
		sort.Strings(vals)
		b.WriteString("\n" + k + ":" + strings.Join(vals, ","))
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(b.String()))
	req.Header.Set("Authorization", "SharedKey "+account+":"+base64.StdEncoding.EncodeToString(m.Sum(nil)))
	return nil
}

var cosmosTypes = map[string]bool{"dbs": true, "colls": true, "sprocs": true, "udfs": true,
	"triggers": true, "users": true, "permissions": true, "docs": true, "pkranges": true, "offers": true}

// cosmosResource returns the resource type and link of a Cosmos DB path:
// the last type segment, and the path through the id after it (or up to
// the type when the path ends at it, as for a list).
func cosmosResource(path string) (string, string) {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if !cosmosTypes[segs[i]] {
			continue
		}
		if i == len(segs)-1 {
			return segs[i], strings.Join(segs[:i], "/")
		}
		return segs[i], strings.Join(segs[:i+2], "/")
	}
	return "", ""
}

// signAzureCosmos signs a Cosmos DB request with the master key.
func signAzureCosmos(req *http.Request, a *AzureCosmosInject, sc Scope) error {
	key, err := azureKey(a.Key, sc)
	if err != nil {
		return fmt.Errorf("azure_cosmos: %w", err)
	}
	date := strings.ToLower(azureNow().Format(http.TimeFormat))
	req.Header.Set("x-ms-date", azureNow().Format(http.TimeFormat))
	p, _ := url.PathUnescape(req.URL.EscapedPath())
	typ, link := cosmosResource(p)
	msg := strings.ToLower(req.Method) + "\n" + strings.ToLower(typ) + "\n" + link + "\n" + date + "\n\n"
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	sig := base64.StdEncoding.EncodeToString(m.Sum(nil))
	req.Header.Set("authorization", url.QueryEscape("type=master&ver=1.0&sig="+sig))
	return nil
}
