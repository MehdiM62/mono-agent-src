package profileinfo

import "testing"

func TestTypeDefaultsToPersonAndSwitchesToBusiness(t *testing.T) {
	db, _ := newDB(t)
	if got, _ := TypeOf(bg, db, "p1"); got != ScopePerson {
		t.Fatalf("default type = %q", got)
	}
	if err := Set(bg, db, "p1", TypeKey, "business", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := TypeOf(bg, db, "p1"); got != ScopeBusiness {
		t.Errorf("type = %q", got)
	}
	if err := Set(bg, db, "p1", TypeKey, "Company", SourceUser, ""); err == nil {
		t.Error("an unknown profile type was accepted")
	}
}

func TestEveryGroupHasAKnownScope(t *testing.T) {
	for _, g := range Groups {
		if g.Scope != "" && g.Scope != ScopePerson && g.Scope != ScopeBusiness {
			t.Errorf("group %s: scope %q", g.ID, g.Scope)
		}
	}
}

func TestBusinessProfileIsOnlyAskedBusinessAndSharedFields(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", TypeKey, "Business", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	fields, err := Missing(bg, db, "p1")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range fields {
		have[f.Key] = true
	}
	for _, k := range []string{"vat_id", "company_name", "owner_name", "owner_tax_id", "iban", "website"} {
		if !have[k] {
			t.Errorf("a business profile is not asked for %s", k)
		}
	}
	for _, k := range []string{"first_name", "passport_number", "employer", TypeKey} {
		if have[k] {
			t.Errorf("a business profile is asked for %s", k)
		}
	}
}

func TestPersonProfileIsNotAskedBusinessFields(t *testing.T) {
	db, _ := newDB(t)
	fields, _ := Missing(bg, db, "p1")
	for _, f := range fields {
		if f.Scope() == ScopeBusiness {
			t.Errorf("a person is asked for %s", f.Key)
		}
	}
}

func TestBusinessValuesSurviveSwitchingBackToPerson(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", "vat_id", "DE123456789", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(bg, db, "p1", false)
	found := false
	for _, e := range got {
		found = found || (e.Key == "vat_id" && e.Value == "DE123456789")
	}
	if !found {
		t.Error("vat_id was not kept")
	}
}
