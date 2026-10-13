package dbnodes

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// SnowflakeNode runs SQL through Snowflake's SQL REST API (/api/v2/statements)
// instead of the gosnowflake driver, which would add ~35 MB to the binary.
// Auth: key-pair JWT (private_key) or a bearer token (token + token_type
// OAUTH or PROGRAMMATIC_ACCESS_TOKEN). The SQL API does not accept plain
// passwords.
// Type: "db.snowflake"
type SnowflakeNode struct{}

func (n *SnowflakeNode) Type() string                  { return "db.snowflake" }
func (n *SnowflakeNode) PerItemConfigFields() []string { return sqlPerItemFields }

// Snowflake response caps, matching the service nodes.
const (
	snowflakeMaxBody      = 64 << 20
	snowflakeMaxErrorEcho = 64 << 10
	snowflakePollInterval = 500 * time.Millisecond
)

var snowflakeHTTP = &http.Client{Timeout: 5 * time.Minute}

func (n *SnowflakeNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	c, err := newSnowflakeClient(config)
	if err != nil {
		return nil, fmt.Errorf("db.snowflake: %w", err)
	}
	if cfgString(config, "operation") == "" {
		cfg := make(map[string]interface{}, len(config)+1)
		for k, v := range config {
			cfg[k] = v
		}
		cfg["operation"] = "executeQuery"
		config = cfg
	}
	return runSQLOps(ctx, "db.snowflake", snowflakeDialect, c, input, config)
}

type snowflakeClient struct {
	baseURL   string
	token     string
	tokenType string
	database  string
	schema    string
	warehouse string
	role      string
	timeout   int
}

func newSnowflakeClient(config map[string]interface{}) (*snowflakeClient, error) {
	account := cfgString(config, "account")
	if account == "" {
		return nil, fmt.Errorf("'account' is required (e.g. myorg-myaccount or xy12345.us-east-1)")
	}
	c := &snowflakeClient{
		baseURL:   strings.TrimRight(cfgString(config, "base_url"), "/"),
		database:  cfgString(config, "database"),
		schema:    cfgString(config, "schema"),
		warehouse: cfgString(config, "warehouse"),
		role:      cfgString(config, "role"),
		timeout:   60,
	}
	if t, ok := cfgInt(config, "timeout_seconds"); ok && t > 0 {
		c.timeout = t
	}
	if c.baseURL == "" {
		c.baseURL = "https://" + account + ".snowflakecomputing.com"
	}
	user := cfgString(config, "username")
	if user == "" {
		user = cfgString(config, "user")
	}
	switch {
	case cfgString(config, "private_key") != "":
		if user == "" {
			return nil, fmt.Errorf("'username' is required for key-pair auth")
		}
		tok, err := snowflakeJWT(account, user, cfgString(config, "private_key"), time.Now())
		if err != nil {
			return nil, fmt.Errorf("'private_key': %w", err)
		}
		c.token, c.tokenType = tok, "KEYPAIR_JWT"
	case cfgString(config, "token") != "":
		c.token = cfgString(config, "token")
		c.tokenType = strings.ToUpper(cfgString(config, "token_type"))
		if c.tokenType == "" {
			c.tokenType = "OAUTH"
		}
	default:
		return nil, fmt.Errorf("'private_key' (key-pair auth) or 'token' (OAuth / programmatic access token) is required")
	}
	return c, nil
}

// snowflakeJWT signs the key-pair JWT the SQL API expects: issuer
// ACCOUNT.USER.SHA256:<public key fingerprint>, subject ACCOUNT.USER.
func snowflakeJWT(account, user, privatePEM string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return "", fmt.Errorf("not a PEM private key")
	}
	if strings.Contains(block.Type, "ENCRYPTED") {
		return "", fmt.Errorf("encrypted private keys are not supported; export the key unencrypted")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("key is not an RSA key")
		}
		key = rk
	} else if rk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
		key = rk
	} else {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pub)
	acct := strings.ToUpper(account)
	if i := strings.IndexByte(acct, '.'); i >= 0 { // locator.region -> locator
		acct = acct[:i]
	}
	qualified := acct + "." + strings.ToUpper(user)
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]interface{}{
		"iss": qualified + ".SHA256:" + base64.StdEncoding.EncodeToString(sum[:]),
		"sub": qualified,
		"iat": now.Unix(),
		"exp": now.Add(59 * time.Minute).Unix(),
	}
	enc := func(v interface{}) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// snowflakeResponse is the subset of the SQL API result we read.
type snowflakeResponse struct {
	Code               string          `json:"code"`
	Message            string          `json:"message"`
	StatementHandle    string          `json:"statementHandle"`
	Data               [][]*string     `json:"data"`
	ResultSetMetaData  *snowflakeMeta  `json:"resultSetMetaData"`
	Stats              *snowflakeStats `json:"stats"`
	StatementStatusURL string          `json:"statementStatusUrl"`
}

type snowflakeMeta struct {
	RowType []struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Scale int    `json:"scale"`
	} `json:"rowType"`
	PartitionInfo []json.RawMessage `json:"partitionInfo"`
}

type snowflakeStats struct {
	Inserted int64 `json:"numRowsInserted"`
	Updated  int64 `json:"numRowsUpdated"`
	Deleted  int64 `json:"numRowsDeleted"`
}

func (c *snowflakeClient) run(ctx context.Context, st sqlStmt) (sqlResult, error) {
	body := map[string]interface{}{"statement": st.Query, "timeout": c.timeout}
	for k, v := range map[string]string{"database": c.database, "schema": c.schema, "warehouse": c.warehouse, "role": c.role} {
		if v != "" {
			body[k] = v
		}
	}
	if len(st.Args) > 0 {
		binds := make(map[string]interface{}, len(st.Args))
		for i, a := range st.Args {
			binds[strconv.Itoa(i+1)] = snowflakeBinding(a)
		}
		body["bindings"] = binds
	}
	resp, err := c.do(ctx, http.MethodPost, c.baseURL+"/api/v2/statements", body)
	if err != nil {
		return sqlResult{}, err
	}
	for resp.ResultSetMetaData == nil && resp.StatementHandle != "" { // 202: still running
		select {
		case <-ctx.Done():
			return sqlResult{}, ctx.Err()
		case <-time.After(snowflakePollInterval):
		}
		if resp, err = c.do(ctx, http.MethodGet, c.baseURL+"/api/v2/statements/"+resp.StatementHandle, nil); err != nil {
			return sqlResult{}, err
		}
	}
	return c.collect(ctx, resp)
}

func (c *snowflakeClient) collect(ctx context.Context, resp *snowflakeResponse) (sqlResult, error) {
	var out sqlResult
	if resp.Stats != nil {
		out.Affected = resp.Stats.Inserted + resp.Stats.Updated + resp.Stats.Deleted
	}
	if resp.ResultSetMetaData == nil {
		return out, nil
	}
	meta := resp.ResultSetMetaData
	for _, rt := range meta.RowType {
		out.Columns = append(out.Columns, rt.Name)
	}
	addRows := func(data [][]*string) {
		for _, rec := range data {
			row := make(map[string]interface{}, len(rec))
			for i, v := range rec {
				if i < len(meta.RowType) {
					row[meta.RowType[i].Name] = snowflakeValue(v, meta.RowType[i].Type, meta.RowType[i].Scale)
				}
			}
			out.Rows = append(out.Rows, row)
		}
	}
	addRows(resp.Data)
	for p := 1; p < len(meta.PartitionInfo); p++ {
		part, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/api/v2/statements/%s?partition=%d", c.baseURL, resp.StatementHandle, p), nil)
		if err != nil {
			return sqlResult{}, fmt.Errorf("partition %d: %w", p, err)
		}
		addRows(part.Data)
	}
	return out, nil
}

func (c *snowflakeClient) do(ctx context.Context, method, url string, body interface{}) (*snowflakeResponse, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Snowflake-Authorization-Token-Type", c.tokenType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "monoagent")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := snowflakeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, snowflakeMaxBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var out snowflakeResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		msg := out.Message
		if msg == "" {
			msg = string(raw)
			if len(msg) > snowflakeMaxErrorEcho {
				msg = msg[:snowflakeMaxErrorEcho] + "…(truncated)"
			}
		}
		return nil, fmt.Errorf("snowflake API %d: %s", resp.StatusCode, msg)
	}
	if resp.StatusCode == http.StatusOK && out.ResultSetMetaData == nil && out.Data == nil && out.StatementHandle == "" {
		return nil, fmt.Errorf("snowflake API: unexpected response")
	}
	if resp.StatusCode == http.StatusOK && out.ResultSetMetaData == nil {
		// A finished statement with no result set (e.g. some DDL): stop polling.
		out.StatementHandle = ""
	}
	return &out, nil
}

// snowflakeBinding maps a Go value to an SQL API binding.
func snowflakeBinding(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case nil:
		return map[string]interface{}{"type": "TEXT", "value": nil}
	case bool:
		return map[string]interface{}{"type": "BOOLEAN", "value": strconv.FormatBool(t)}
	case int, int64, int32:
		return map[string]interface{}{"type": "FIXED", "value": fmt.Sprint(t)}
	case float64:
		if t == float64(int64(t)) {
			return map[string]interface{}{"type": "FIXED", "value": strconv.FormatInt(int64(t), 10)}
		}
		return map[string]interface{}{"type": "REAL", "value": strconv.FormatFloat(t, 'f', -1, 64)}
	case string:
		return map[string]interface{}{"type": "TEXT", "value": t}
	default:
		b, _ := json.Marshal(t)
		return map[string]interface{}{"type": "TEXT", "value": string(b)}
	}
}

// snowflakeValue converts one result cell (always a string or null in the
// SQL API's JSON format) to a typed value.
func snowflakeValue(v *string, typ string, scale int) interface{} {
	if v == nil {
		return nil
	}
	switch strings.ToLower(typ) {
	case "fixed":
		if scale == 0 {
			if i, err := strconv.ParseInt(*v, 10, 64); err == nil {
				return i
			}
		}
		if f, err := strconv.ParseFloat(*v, 64); err == nil {
			return f
		}
	case "real":
		if f, err := strconv.ParseFloat(*v, 64); err == nil {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(*v); err == nil {
			return b
		}
	case "variant", "object", "array":
		var j interface{}
		if json.Unmarshal([]byte(*v), &j) == nil {
			return j
		}
	}
	return *v
}
