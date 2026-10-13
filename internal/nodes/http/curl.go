package httpnodes

import (
	"fmt"
	"mime"
	"net/url"
	"strconv"
	"strings"
)

// CurlRequest is a curl command line read by ParseCurl.
type CurlRequest struct {
	Method      string
	URL         string
	Headers     []CurlHeader
	Data        []string // -d / --data* pieces, already encoded as curl sends them
	JSONBody    bool     // --json
	Form        []CurlFormField
	QueryData   bool // -G: Data goes in the query string
	User        string
	Password    string
	HasUser     bool
	BearerToken string
	Proxy       string
	Timeout     float64
	Insecure    bool
	// Notes are things the import could not carry over or changed.
	Notes []string
}

// CurlHeader is one -H header in command-line order.
type CurlHeader struct{ Name, Value string }

// CurlFormField is one -F field: a value, or File for name=@path.
type CurlFormField struct {
	Name, Value, File, ContentType, Filename string
}

// curlArgFlags are curl options that take an argument we do not use.
var curlArgFlags = map[string]bool{
	"-o": true, "--output": true, "-w": true, "--write-out": true, "--connect-timeout": true,
	"--retry": true, "--retry-delay": true, "--retry-max-time": true, "-c": true, "--cookie-jar": true,
	"--cacert": true, "--cert": true, "--key": true, "-D": true, "--dump-header": true,
	"-r": true, "--range": true, "--limit-rate": true, "--resolve": true, "--interface": true,
	"--max-redirs": true, "-T": true, "--upload-file": true, "-K": true, "--config": true,
	"--trace": true, "--trace-ascii": true, "--stderr": true, "-Y": true, "-y": true,
}

// ParseCurl reads a curl command line (as copied from browser dev tools or
// API docs): quoting, $'...' strings and backslash line continuations are
// understood. The leading "curl" is optional.
func ParseCurl(command string) (*CurlRequest, error) {
	args, err := splitShellWords(command)
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && (args[0] == "curl" || strings.HasSuffix(args[0], "/curl") || args[0] == "curl.exe") {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("curl: the command has no URL")
	}
	c := &CurlRequest{}
	head := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("curl: option %s needs a value", a)
			}
			i++
			return args[i], nil
		}
		flag, val, attached := splitCurlFlag(a)
		if !strings.HasPrefix(a, "-") || a == "-" {
			if c.URL == "" {
				c.URL = a
			} else {
				c.Notes = append(c.Notes, fmt.Sprintf("extra URL %q ignored: a node makes one request", a))
			}
			continue
		}
		// Bundled short flags without a value, e.g. -sSL or -sk.
		if !attached && len(a) > 2 && a[1] != '-' {
			if expanded, ok := expandShortFlags(a); ok {
				args = append(args[:i+1], append(expanded, args[i+1:]...)...)
				continue
			}
		}
		value := func() (string, error) {
			if attached {
				return val, nil
			}
			return next()
		}
		var v string
		switch flag {
		case "-X", "--request", "-H", "--header", "-d", "--data", "--data-ascii", "--data-raw", "--data-binary",
			"--data-urlencode", "--json", "-F", "--form", "--form-string", "-u", "--user", "--url", "-A", "--user-agent",
			"-e", "--referer", "-b", "--cookie", "-x", "--proxy", "-m", "--max-time", "--oauth2-bearer":
			if v, err = value(); err != nil {
				return nil, err
			}
		}
		switch flag {
		case "-X", "--request":
			c.Method = strings.ToUpper(v)
		case "-H", "--header":
			name, hv, ok := strings.Cut(v, ":")
			if !ok {
				if strings.HasSuffix(v, ";") {
					c.Headers = append(c.Headers, CurlHeader{Name: strings.TrimSuffix(v, ";")})
				} else {
					c.Notes = append(c.Notes, fmt.Sprintf("header %q has no value and was skipped", v))
				}
				continue
			}
			c.Headers = append(c.Headers, CurlHeader{Name: strings.TrimSpace(name), Value: strings.TrimSpace(hv)})
		case "-d", "--data", "--data-ascii", "--data-binary":
			if strings.HasPrefix(v, "@") {
				c.Notes = append(c.Notes, fmt.Sprintf("body file %s was not read: paste its content into body", v))
			}
			if flag != "--data-binary" {
				v = strings.NewReplacer("\r", "", "\n", "").Replace(v)
			}
			c.Data = append(c.Data, v)
		case "--data-raw":
			c.Data = append(c.Data, v)
		case "--json":
			c.Data = append(c.Data, v)
			c.JSONBody = true
		case "--data-urlencode":
			c.Data = append(c.Data, curlURLEncodeData(v, &c.Notes))
		case "-F", "--form", "--form-string":
			f, err := parseCurlFormField(v, flag == "--form-string")
			if err != nil {
				return nil, err
			}
			c.Form = append(c.Form, f)
		case "-u", "--user":
			c.User, c.Password, _ = strings.Cut(v, ":")
			c.HasUser = true
		case "--oauth2-bearer":
			c.BearerToken = v
		case "--url":
			c.URL = v
		case "-G", "--get":
			c.QueryData = true
		case "-I", "--head":
			head = true
		case "-A", "--user-agent":
			c.Headers = append(c.Headers, CurlHeader{Name: "User-Agent", Value: v})
		case "-e", "--referer":
			c.Headers = append(c.Headers, CurlHeader{Name: "Referer", Value: v})
		case "-b", "--cookie":
			if strings.Contains(v, "=") {
				c.Headers = append(c.Headers, CurlHeader{Name: "Cookie", Value: v})
			} else {
				c.Notes = append(c.Notes, fmt.Sprintf("cookie file %q was not read", v))
			}
		case "-x", "--proxy":
			c.Proxy = v
		case "-m", "--max-time":
			if t, err := strconv.ParseFloat(v, 64); err == nil && t > 0 {
				c.Timeout = t
			}
		case "-k", "--insecure":
			c.Insecure = true
			c.Notes = append(c.Notes, "-k/--insecure ignored: certificates are verified; set allow_unauthorized_certs to true to skip that")
		case "-L", "--location", "--compressed", "-s", "--silent", "-S", "--show-error", "-v", "--verbose",
			"-i", "--include", "-f", "--fail", "--fail-with-body", "-g", "--globoff", "-N", "--no-buffer",
			"--http1.0", "--http1.1", "--http2", "--http2-prior-knowledge", "--http3", "-#", "--progress-bar",
			"-n", "--netrc", "--location-trusted", "-0", "-1", "-2", "-3", "-4", "-6", "-q", "--disable":
		default:
			if curlArgFlags[flag] {
				if !attached {
					if _, err := next(); err != nil {
						return nil, err
					}
				}
			}
			c.Notes = append(c.Notes, fmt.Sprintf("option %s ignored", flag))
		}
	}
	if c.URL == "" {
		return nil, fmt.Errorf("curl: the command has no URL")
	}
	if !strings.Contains(c.URL, "://") {
		c.URL = "http://" + c.URL
	}
	if _, err := url.Parse(c.URL); err != nil {
		return nil, fmt.Errorf("curl: URL %q: %v", c.URL, err)
	}
	if len(c.Data) > 0 && !c.JSONBody && !c.QueryData && len(c.Form) == 0 && c.contentType() == "" && looksLikeJSON(strings.Join(c.Data, "&")) {
		c.Notes = append(c.Notes, "the data looks like JSON and no Content-Type was given: it is sent as JSON (curl would label it form-urlencoded)")
	}
	if c.Method == "" {
		switch {
		case head:
			c.Method = "HEAD"
		case (len(c.Data) > 0 && !c.QueryData) || len(c.Form) > 0:
			c.Method = "POST"
		default:
			c.Method = "GET"
		}
	}
	return c, nil
}

// splitCurlFlag splits "--data=x" and "-XPOST" / "-H'X: y'" forms.
func splitCurlFlag(a string) (flag, value string, attached bool) {
	if strings.HasPrefix(a, "--") {
		if f, v, ok := strings.Cut(a, "="); ok {
			return f, v, true
		}
		return a, "", false
	}
	if len(a) > 2 && strings.HasPrefix(a, "-") {
		switch a[:2] {
		case "-X", "-H", "-d", "-F", "-u", "-A", "-e", "-b", "-x", "-m", "-o", "-w":
			return a[:2], a[2:], true
		}
	}
	return a, "", false
}

// expandShortFlags turns -sSL into -s -S -L when every letter is a flag
// that takes no value; a value-taking letter last (-sX) takes the next arg.
func expandShortFlags(a string) ([]string, bool) {
	const noValue = "sSLkvifgIGNnq0123460#"
	const withValue = "XHdFuAebxmowcDrTKYy"
	var out []string
	for i, r := range a[1:] {
		switch {
		case strings.ContainsRune(noValue, r):
			out = append(out, "-"+string(r))
		case strings.ContainsRune(withValue, r) && i == len(a)-2:
			out = append(out, "-"+string(r))
		default:
			return nil, false
		}
	}
	return out, true
}

// curlURLEncodeData encodes one --data-urlencode argument as curl does.
func curlURLEncodeData(v string, notes *[]string) string {
	if name, content, ok := strings.Cut(v, "="); ok {
		if name == "" {
			return url.QueryEscape(content)
		}
		return url.QueryEscape(name) + "=" + url.QueryEscape(content)
	}
	if name, file, ok := strings.Cut(v, "@"); ok {
		*notes = append(*notes, fmt.Sprintf("--data-urlencode file %q was not read", file))
		return url.QueryEscape(name)
	}
	return url.QueryEscape(v)
}

// parseCurlFormField reads -F name=value, name=@file;type=...;filename=...
// and name=<file (contents as a text value, not read here).
func parseCurlFormField(v string, literal bool) (CurlFormField, error) {
	name, rest, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return CurlFormField{}, fmt.Errorf("curl: form field %q is not name=value", v)
	}
	f := CurlFormField{Name: name}
	if literal || !(strings.HasPrefix(rest, "@") || strings.HasPrefix(rest, "<")) {
		f.Value = rest
		if !literal {
			// ;type= on a plain value only sets the part's content type.
			if base, params, ok := strings.Cut(rest, ";type="); ok {
				f.Value, f.ContentType = base, params
			}
		}
		return f, nil
	}
	parts := strings.Split(rest[1:], ";")
	f.File = parts[0]
	for _, p := range parts[1:] {
		k, val, _ := strings.Cut(p, "=")
		switch strings.TrimSpace(k) {
		case "type":
			f.ContentType = val
		case "filename":
			f.Filename = val
		}
	}
	return f, nil
}

// contentType is the last Content-Type header given, or "".
func (c *CurlRequest) contentType() string {
	ct := ""
	for _, h := range c.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			ct = h.Value
		}
	}
	return ct
}

// NodeConfig is the http.request config for the command.
func (c *CurlRequest) NodeConfig() map[string]interface{} {
	cfg := map[string]interface{}{"method": c.Method, "url": c.URL}
	headers := map[string]interface{}{}
	contentType := ""
	for _, h := range c.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			contentType = h.Value
		}
		if prev, ok := headers[h.Name]; ok {
			headers[h.Name] = prev.(string) + ", " + h.Value
			continue
		}
		headers[h.Name] = h.Value
	}
	mt, _, _ := mime.ParseMediaType(contentType)

	data := strings.Join(c.Data, "&")
	switch {
	case len(c.Form) > 0:
		cfg["body_type"] = "multipart"
		cfg["body"] = c.formBody()
		delete(headers, headerKey(headers, "Content-Type"))
	case len(c.Data) > 0 && c.QueryData:
		if q, err := url.ParseQuery(data); err == nil {
			cfg["query_params"] = valuesToConfig(q)
		} else {
			sep := "?"
			if strings.Contains(c.URL, "?") {
				sep = "&"
			}
			cfg["url"] = c.URL + sep + data
		}
	case len(c.Data) > 0:
		if c.JSONBody {
			data = strings.Join(c.Data, "")
			if contentType == "" {
				mt = "application/json"
			}
			if headerKey(headers, "Accept") == "" {
				headers["Accept"] = "application/json"
			}
		}
		switch {
		case mt == "application/json" || strings.HasSuffix(mt, "+json"):
			cfg["body_type"] = "json"
			cfg["body"] = data
			if mt == "application/json" {
				delete(headers, headerKey(headers, "Content-Type"))
			}
		case mt == "" && looksLikeJSON(data):
			cfg["body_type"] = "json"
			cfg["body"] = data
		case mt == "" || mt == "application/x-www-form-urlencoded":
			cfg["body_type"] = "form"
			cfg["body"] = formBodyConfig(data)
			delete(headers, headerKey(headers, "Content-Type"))
		default:
			cfg["body_type"] = "raw"
			cfg["raw_content_type"] = contentType
			cfg["body"] = data
			delete(headers, headerKey(headers, "Content-Type"))
		}
	}
	if len(headers) > 0 {
		cfg["headers"] = headers
	}
	switch {
	case c.HasUser:
		cfg["auth_type"] = "basic"
		cfg["auth_username"] = c.User
		cfg["auth_password"] = c.Password
	case c.BearerToken != "":
		cfg["auth_type"] = "bearer"
		cfg["auth_token"] = c.BearerToken
	}
	if c.Proxy != "" {
		cfg["proxy"] = c.Proxy
	}
	if c.Timeout > 0 {
		cfg["timeout"] = c.Timeout
	}
	return cfg
}

func headerKey(h map[string]interface{}, name string) string {
	for k := range h {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return ""
}

func (c *CurlRequest) formBody() map[string]interface{} {
	body := map[string]interface{}{}
	for _, f := range c.Form {
		if f.File == "" {
			if f.ContentType != "" {
				body[f.Name] = map[string]interface{}{"value": f.Value, "content_type": f.ContentType}
			} else {
				body[f.Name] = f.Value
			}
			continue
		}
		spec := map[string]interface{}{"file": f.File}
		if f.ContentType != "" {
			spec["content_type"] = f.ContentType
		}
		if f.Filename != "" {
			spec["filename"] = f.Filename
		}
		body[f.Name] = spec
	}
	return body
}

// formBodyConfig keeps a urlencoded body readable as an object when it
// decodes cleanly with no repeated names; otherwise it stays as sent.
func formBodyConfig(data string) interface{} {
	q, err := url.ParseQuery(data)
	if err != nil || len(q) == 0 {
		return data
	}
	out := map[string]interface{}{}
	for k, vs := range q {
		if len(vs) != 1 {
			return data
		}
		out[k] = vs[0]
	}
	return out
}

func valuesToConfig(q url.Values) map[string]interface{} {
	out := map[string]interface{}{}
	for k, vs := range q {
		if len(vs) == 1 {
			out[k] = vs[0]
			continue
		}
		list := make([]interface{}, len(vs))
		for i, v := range vs {
			list[i] = v
		}
		out[k] = list
	}
	return out
}
