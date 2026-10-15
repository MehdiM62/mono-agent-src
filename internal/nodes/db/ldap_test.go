package dbnodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-ldap/ldap/v3"

	"github.com/monoes/mono-agent/internal/workflow"
)

type fakeLDAP struct {
	search  *ldap.SearchRequest
	page    uint32
	add     *ldap.AddRequest
	del     *ldap.DelRequest
	modify  *ldap.ModifyRequest
	rename  *ldap.ModifyDNRequest
	compare []string
	entries []*ldap.Entry
}

func (f *fakeLDAP) SearchWithPaging(r *ldap.SearchRequest, p uint32) (*ldap.SearchResult, error) {
	f.search, f.page = r, p
	return &ldap.SearchResult{Entries: f.entries}, nil
}
func (f *fakeLDAP) Add(r *ldap.AddRequest) error           { f.add = r; return nil }
func (f *fakeLDAP) Del(r *ldap.DelRequest) error           { f.del = r; return nil }
func (f *fakeLDAP) Modify(r *ldap.ModifyRequest) error     { f.modify = r; return nil }
func (f *fakeLDAP) ModifyDN(r *ldap.ModifyDNRequest) error { f.rename = r; return nil }
func (f *fakeLDAP) Compare(dn, a, v string) (bool, error) {
	f.compare = []string{dn, a, v}
	return v == "yes", nil
}
func (f *fakeLDAP) Close() error { return nil }

func runLDAP(t *testing.T, f *fakeLDAP, input workflow.NodeInput, cfg map[string]interface{}) []workflow.Item {
	t.Helper()
	prev := ldapDial
	ldapDial = func(map[string]interface{}) (ldapConn, error) { return f, nil }
	defer func() { ldapDial = prev }()
	cfg["hostname"] = "dir"
	out, err := (&LDAPNode{}).Execute(context.Background(), input, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return mainItems(t, out)
}

func TestLDAPSearchFilterAndEntries(t *testing.T) {
	f := &fakeLDAP{entries: []*ldap.Entry{{DN: "cn=ada,dc=x", Attributes: []*ldap.EntryAttribute{
		{Name: "cn", ByteValues: [][]byte{[]byte("ada")}},
		{Name: "mail", ByteValues: [][]byte{[]byte("a@x"), []byte("ada@x")}},
		{Name: "objectGUID", ByteValues: [][]byte{{0xff, 0xfe}}},
	}}}}
	got := runLDAP(t, f, items(map[string]interface{}{"q": "a(d)a*"}), map[string]interface{}{
		"operation": "search", "base_dn": "dc=x", "search_for": "(objectclass=person)", "attribute": "cn",
		"search_text": "{{ $json.q }}", "scope": "one", "limit": float64(10), "return_attributes": "cn,mail",
	})
	if f.search.Filter != `(&(objectclass=person)(cn=a\28d\29a*))` || f.search.Scope != ldap.ScopeSingleLevel || f.search.SizeLimit != 10 || f.page != 1000 {
		t.Errorf("search = %+v", f.search)
	}
	if !reflect.DeepEqual(f.search.Attributes, []string{"cn", "mail"}) {
		t.Errorf("attributes = %v", f.search.Attributes)
	}
	want := map[string]interface{}{"dn": "cn=ada,dc=x", "cn": "ada", "mail": []interface{}{"a@x", "ada@x"}, "objectGUID": "//4="}
	if len(got) != 1 || !reflect.DeepEqual(got[0].JSON, want) {
		t.Errorf("got %#v", got)
	}
	runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "search", "base_dn": "dc=x", "search_for": "custom", "custom_filter": "(uid=1)", "return_all": true})
	if f.search.Filter != "(uid=1)" || f.search.SizeLimit != 0 {
		t.Errorf("custom search = %+v", f.search)
	}
}

func TestLDAPWriteOperations(t *testing.T) {
	f := &fakeLDAP{}
	runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "create", "dn": "cn=bob,dc=x",
		"attributes": []interface{}{map[string]interface{}{"id": "objectClass", "value": "person"}, map[string]interface{}{"id": "objectClass", "value": "top"}, map[string]interface{}{"id": "sn", "value": "B"}}})
	if f.add.DN != "cn=bob,dc=x" || len(f.add.Attributes) != 2 || !reflect.DeepEqual(f.add.Attributes[0].Vals, []string{"person", "top"}) {
		t.Errorf("add = %+v", f.add)
	}
	runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "update", "dn": "cn=bob,dc=x", "attributes": map[string]interface{}{
		"replace": []interface{}{map[string]interface{}{"id": "mail", "value": "b@x"}},
		"delete":  []interface{}{map[string]interface{}{"id": "phone", "value": "1"}},
	}})
	if len(f.modify.Changes) != 2 || f.modify.Changes[0].Operation != ldap.ReplaceAttribute || f.modify.Changes[1].Operation != ldap.DeleteAttribute {
		t.Errorf("modify = %+v", f.modify)
	}
	got := runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "rename", "dn": "cn=bob,ou=a,dc=x", "target_dn": "cn=robert,ou=b,dc=x"})
	if f.rename.NewRDN != "cn=robert" || f.rename.NewSuperior != "ou=b,dc=x" || !f.rename.DeleteOldRDN || got[0].JSON["dn"] != "cn=robert,ou=b,dc=x" {
		t.Errorf("rename = %+v", f.rename)
	}
	runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "rename", "dn": "cn=bob,ou=a,dc=x", "target_dn": "cn=rob,ou=a,dc=x"})
	if f.rename.NewSuperior != "" {
		t.Errorf("same-parent rename must not move: %+v", f.rename)
	}
	runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "delete", "dn": "cn=rob,ou=a,dc=x"})
	if f.del.DN != "cn=rob,ou=a,dc=x" {
		t.Errorf("del = %+v", f.del)
	}
	got = runLDAP(t, f, workflow.NodeInput{}, map[string]interface{}{"operation": "compare", "dn": "cn=a", "attribute_id": "x", "value": "yes"})
	if got[0].JSON["result"] != true || f.compare[1] != "x" {
		t.Errorf("compare = %v %v", got, f.compare)
	}
}

func TestLDAPErrors(t *testing.T) {
	f := &fakeLDAP{}
	prev := ldapDial
	ldapDial = func(map[string]interface{}) (ldapConn, error) { return f, nil }
	defer func() { ldapDial = prev }()
	for _, cfg := range []map[string]interface{}{
		{},
		{"operation": "delete"},
		{"operation": "update", "dn": "cn=a"},
		{"operation": "search"},
		{"operation": "teleport", "dn": "cn=a"},
	} {
		if _, err := (&LDAPNode{}).Execute(context.Background(), workflow.NodeInput{}, cfg); err == nil {
			t.Errorf("config %v: want error", cfg)
		}
	}
}
