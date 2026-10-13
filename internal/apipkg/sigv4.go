package apipkg

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// sigV4Params are the resolved inputs of an AWS Signature Version 4.
type sigV4Params struct {
	Service, Region, AccessKeyID, SecretAccessKey, SessionToken string
}

// signSigV4 signs req (whose body is body) with AWS SigV4 at time now. It
// sets X-Amz-Date, X-Amz-Security-Token (with a session token),
// X-Amz-Content-Sha256 (S3) and Authorization.
func signSigV4(req *http.Request, body []byte, p sigV4Params, now time.Time) error {
	if p.AccessKeyID == "" || p.SecretAccessKey == "" {
		return fmt.Errorf("aws_sigv4: the credential has no access key id or secret access key")
	}
	if p.Region == "" || p.Service == "" {
		return fmt.Errorf("aws_sigv4: service and region are required")
	}
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	payloadHash := sha256Hex(body)

	req.Header.Set("X-Amz-Date", amzDate)
	if p.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", p.SessionToken)
	}
	if p.Service == "s3" {
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if lk == "content-type" || strings.HasPrefix(lk, "x-amz-") {
			headers[lk] = strings.Join(trimAll(vs), ",")
		}
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL, p.Service),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")

	scope := day + "/" + p.Region + "/" + p.Service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))

	k := hmacSHA256([]byte("AWS4"+p.SecretAccessKey), day)
	k = hmacSHA256(k, p.Region)
	k = hmacSHA256(k, p.Service)
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.AccessKeyID, scope, signed, sig))
	return nil
}

func trimAll(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strings.Join(strings.Fields(v), " ")
	}
	return out
}

// canonicalURI encodes each path segment: once for S3, twice otherwise
// (the URL's escaped path is the first encoding).
func canonicalURI(u *url.URL, service string) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	if service == "s3" {
		return p
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = awsEscape(s)
	}
	return strings.Join(segs, "/")
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, awsEscape(k)+"="+awsEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape is RFC 3986 percent-encoding: only unreserved characters stay.
func awsEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
