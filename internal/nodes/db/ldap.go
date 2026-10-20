package dbnodes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"

	"github.com/monoes/mono-agent/internal/workflow"
)

// ldapConn is the part of *ldap.Conn the node uses; tests substitute a fake.
type ldapConn interface {
	SearchWithPaging(req *ldap.SearchRequest, pagingSize uint32) (*ldap.SearchResult, error)
	Add(*ldap.AddRequest) error
	Del(*ldap.DelRequest) error
	Modify(*ldap.ModifyRequest) error
	ModifyDN(*ldap.ModifyDNRequest) error
	Compare(dn, attribute, value string) (bool, error)
	Close() error
}

// ldapDial connects and binds from the credential keys (hostname, port,
// bind_dn, bind_password, connection_security none|tls|startTLS,
// allow_unauthorized_certs, ca_certificate, timeout seconds). Tests swap it.
var ldapDial = func(cfg map[string]interface{}) (ldapConn, error) {
	host := cfgString(cfg, "hostname")
	if host == "" {
		return nil, fmt.Errorf("'hostname' is required")
	}
	security := strings.ToLower(cfgString(cfg, "connection_security"))
	port, ok := cfgInt(cfg, "port")
	if !ok || port == 0 {
		port = 389
		if security == "tls" {
			port = 636
		}
	}
	tc := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if cfgBool(cfg, "allow_unauthorized_certs", false) {
		tc.InsecureSkipVerify = true //nolint:gosec // explicit user opt-in
	}
	if ca := cfgString(cfg, "ca_certificate"); ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("'ca_certificate' is not a PEM certificate")
		}
		tc.RootCAs = pool
	}
	timeout := 300 * time.Second
	if s, ok := cfgInt(cfg, "timeout"); ok && s > 0 {
		timeout = time.Duration(s) * time.Second
	}
	scheme := "ldap"
	if security == "tls" {
		scheme = "ldaps"
	}
	conn, err := ldap.DialURL(scheme+"://"+host+":"+strconv.Itoa(port), ldap.DialWithTLSConfig(tc))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	conn.SetTimeout(timeout)
	if security == "starttls" {
		if err := conn.StartTLS(tc); err != nil {
			conn.Close()
			return nil, fmt.Errorf("StartTLS: %w", err)
		}
	}
	if dn := cfgString(cfg, "bind_dn"); dn != "" {
		if err := conn.Bind(dn, cfgString(cfg, "bind_password")); err != nil {
			conn.Close()
			return nil, fmt.Errorf("bind: %w", err)
		}
	}
	return conn, nil
}

var ldapPerItemFields = []string{"dn", "target_dn", "attribute_id", "value", "attributes", "base_dn", "search_text", "custom_filter", "search_for"}

// LDAPNode runs directory operations (compare, create, delete, rename,
// search, update) once per input item. It is named db.ldap so the existing
// "db." plumbing (credential platform lookup, outbound-node policy, API
// category) applies to it unchanged.
// Type: "db.ldap"
type LDAPNode struct{}

func (n *LDAPNode) Type() string { return "db.ldap" }

// CredentialDestinationKeys: the directory the bind password goes to
// (workflow.CheckCredentialDestinations).
func (n *LDAPNode) CredentialDestinationKeys() [][]string { return [][]string{{"hostname"}, {"port"}} }
func (n *LDAPNode) PerItemConfigFields() []string         { return ldapPerItemFields }

func (n *LDAPNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	op := cfgString(config, "operation")
	if op == "" {
		return nil, fmt.Errorf("db.ldap: 'operation' is required (compare, create, delete, rename, search or update)")
	}
	conn, err := ldapDial(config)
	if err != nil {
		return nil, fmt.Errorf("db.ldap: %w", err)
	}
	defer conn.Close()
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, ldapPerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("db.ldap: %w", err)
		}
		res, err := runLDAPOp(conn, op, cfg)
		if err != nil {
			return nil, fmt.Errorf("db.ldap: %s (item %d): %w", op, i, err)
		}
		out = append(out, res...)
	}
	return mainOutputs(out, nil), nil
}

func runLDAPOp(conn ldapConn, op string, cfg map[string]interface{}) ([]workflow.Item, error) {
	dn := cfgString(cfg, "dn")
	needDN := func() error {
		if dn == "" {
			return fmt.Errorf("'dn' is required")
		}
		return nil
	}
	ok := func(d string) []workflow.Item {
		return []workflow.Item{workflow.NewItem(map[string]interface{}{"dn": d, "result": "success"})}
	}
	switch op {
	case "compare":
		if err := needDN(); err != nil {
			return nil, err
		}
		attr := cfgString(cfg, "attribute_id")
		if attr == "" {
			return nil, fmt.Errorf("'attribute_id' is required")
		}
		res, err := conn.Compare(dn, attr, cfgString(cfg, "value"))
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{"dn": dn, "attribute": attr, "result": res})}, nil

	case "create":
		if err := needDN(); err != nil {
			return nil, err
		}
		attrs, err := ldapAttrList(cfg["attributes"])
		if err != nil {
			return nil, err
		}
		req := ldap.NewAddRequest(dn, nil)
		for _, a := range attrs {
			req.Attribute(a.Type, a.Vals)
		}
		return ok(dn), conn.Add(req)

	case "delete":
		if err := needDN(); err != nil {
			return nil, err
		}
		return ok(dn), conn.Del(ldap.NewDelRequest(dn, nil))

	case "rename":
		if err := needDN(); err != nil {
			return nil, err
		}
		target := cfgString(cfg, "target_dn")
		if target == "" {
			return nil, fmt.Errorf("'target_dn' is required")
		}
		req, err := ldapRenameRequest(dn, target)
		if err != nil {
			return nil, err
		}
		return ok(target), conn.ModifyDN(req)

	case "update":
		if err := needDN(); err != nil {
			return nil, err
		}
		changes, err := toMap(cfg["attributes"])
		if err != nil {
			return nil, fmt.Errorf("'attributes': %w", err)
		}
		req := ldap.NewModifyRequest(dn, nil)
		n := 0
		for _, action := range []string{"add", "replace", "delete"} {
			attrs, err := ldapAttrList(changes[action])
			if err != nil {
				return nil, fmt.Errorf("'attributes.%s': %w", action, err)
			}
			for _, a := range attrs {
				switch action {
				case "add":
					req.Add(a.Type, a.Vals)
				case "replace":
					req.Replace(a.Type, a.Vals)
				case "delete":
					req.Delete(a.Type, a.Vals)
				}
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("'attributes' needs at least one add, replace or delete entry")
		}
		return ok(dn), conn.Modify(req)

	case "search":
		return ldapSearch(conn, cfg)
	}
	return nil, fmt.Errorf("unknown operation %q (want compare, create, delete, rename, search or update)", op)
}

// ldapAttrList reads [{id, value}] (values may repeat an id) into attributes.
func ldapAttrList(v interface{}) ([]ldap.Attribute, error) {
	list, err := toList(v)
	if err != nil {
		if m, ok := v.(map[string]interface{}); ok && m["attribute"] != nil { // n8n {attribute: [...]}
			return ldapAttrList(m["attribute"])
		}
		return nil, fmt.Errorf("'attributes': %w", err)
	}
	var out []ldap.Attribute
	index := map[string]int{}
	for i, e := range list {
		m, ok := e.(map[string]interface{})
		if !ok || cfgString(m, "id") == "" {
			return nil, fmt.Errorf("'attributes'[%d]: expected {\"id\": ..., \"value\": ...}", i)
		}
		id := cfgString(m, "id")
		var vals []string
		if arr, ok := m["value"].([]interface{}); ok {
			for _, x := range arr {
				vals = append(vals, redisString(x))
			}
		} else {
			vals = []string{redisString(m["value"])}
		}
		if j, seen := index[id]; seen {
			out[j].Vals = append(out[j].Vals, vals...)
			continue
		}
		index[id] = len(out)
		out = append(out, ldap.Attribute{Type: id, Vals: vals})
	}
	return out, nil
}

func ldapRenameRequest(dn, target string) (*ldap.ModifyDNRequest, error) {
	t, err := ldap.ParseDN(target)
	if err != nil || len(t.RDNs) == 0 {
		return nil, fmt.Errorf("'target_dn' is not a valid DN: %v", err)
	}
	newRDN := strings.SplitN(target, ",", 2)[0]
	newParent := ""
	if parts := strings.SplitN(target, ",", 2); len(parts) == 2 {
		newParent = strings.TrimSpace(parts[1])
	}
	oldParent := ""
	if parts := strings.SplitN(dn, ",", 2); len(parts) == 2 {
		oldParent = strings.TrimSpace(parts[1])
	}
	if strings.EqualFold(newParent, oldParent) {
		newParent = ""
	}
	return ldap.NewModifyDNRequest(dn, newRDN, true, newParent), nil
}

// ldapFilterValue escapes a search value but keeps '*' as a wildcard.
func ldapFilterValue(s string) string {
	parts := strings.Split(s, "*")
	for i, p := range parts {
		parts[i] = ldap.EscapeFilter(p)
	}
	return strings.Join(parts, "*")
}

func ldapSearch(conn ldapConn, cfg map[string]interface{}) ([]workflow.Item, error) {
	base := cfgString(cfg, "base_dn")
	if base == "" {
		return nil, fmt.Errorf("'base_dn' is required")
	}
	searchFor := cfgString(cfg, "search_for")
	var filter string
	if searchFor == "custom" || (searchFor == "" && cfgString(cfg, "custom_filter") != "") {
		filter = cfgString(cfg, "custom_filter")
		if filter == "" {
			filter = "(objectclass=*)"
		}
	} else {
		attr := cfgString(cfg, "attribute")
		if attr == "" {
			return nil, fmt.Errorf("'attribute' is required unless search_for is custom")
		}
		filter = fmt.Sprintf("(&%s(%s=%s))", searchFor, ldap.EscapeFilter(attr), ldapFilterValue(cfgString(cfg, "search_text")))
	}
	scope := ldap.ScopeWholeSubtree
	switch cfgString(cfg, "scope") {
	case "base":
		scope = ldap.ScopeBaseObject
	case "one":
		scope = ldap.ScopeSingleLevel
	}
	limit := 0
	if !cfgBool(cfg, "return_all", false) {
		limit = 50
		if l, ok := cfgInt(cfg, "limit"); ok {
			limit = l
		}
	}
	pageSize := 1000
	if p, ok := cfgInt(cfg, "page_size"); ok && p > 0 {
		pageSize = p
	}
	req := ldap.NewSearchRequest(base, scope, ldap.NeverDerefAliases, limit, 0, false, filter, toStringList(cfg["return_attributes"]), nil)
	res, err := conn.SearchWithPaging(req, uint32(pageSize))
	if err != nil && !(ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) && res != nil) {
		return nil, err
	}
	entries := res.Entries
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]workflow.Item, 0, len(entries))
	for _, e := range entries {
		data := map[string]interface{}{"dn": e.DN}
		for _, a := range e.Attributes {
			vals := make([]interface{}, len(a.ByteValues))
			for i, b := range a.ByteValues {
				if utf8.Valid(b) {
					vals[i] = string(b)
				} else {
					vals[i] = base64.StdEncoding.EncodeToString(b)
				}
			}
			if len(vals) == 1 {
				data[a.Name] = vals[0]
			} else {
				data[a.Name] = vals
			}
		}
		out = append(out, workflow.NewItem(data))
	}
	return out, nil
}
