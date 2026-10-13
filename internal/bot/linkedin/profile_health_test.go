//go:build !nosocial

package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// healthProfilePage scripts a profile page: the top card, and the
// Experience/Education sections — from the profile page itself, or from the
// /details/<kind>/ pages when details is set.
func healthProfilePage(t *testing.T, details map[string]interface{}) *healthPage {
	exp, edu := healthFixture(t, "section_experience"), healthFixture(t, "section_education")
	return newHealthPage(t).scriptCommon().
		onValue(hsProfile, healthFixture(t, "profile")).
		on(hsSection, func(c healthCall) interface{} {
			kind, _ := c.Args[0].(string)
			if strings.Contains(c.URL, "/details/") {
				if v, ok := details[kind]; ok {
					return v
				}
				return map[string]interface{}{"found": false, "entries": []interface{}{}}
			}
			if kind == "education" {
				return edu
			}
			return exp
		})
}

func TestHealthGetProfileDataMapsTopCardAndSections(t *testing.T) {
	healthTimings(t)
	p := healthProfilePage(t, nil)
	m, err := (&LinkedInBot{}).GetProfileData(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"username":            "ann-lee-test",
		"profile_url":         "https://www.linkedin.com/in/ann-lee-test/", // query and fragment dropped
		"full_name":           "Ann Lee",
		"headline":            "Staff Engineer at Acme",
		"location":            "Berlin, Germany",
		"about":               "Builds reliable things.",
		"connection_count":    "500+",
		"follower_count":      "1,234",
		"connection_degree":   "2nd",
		"profile_picture_url": "https://media.licdn.com/dms/image/profile-displayphoto.jpg",
		"cover_image_url":     "https://media.licdn.com/dms/image/profile-displaybackgroundimage.jpg",
		"is_self":             false,
		"job_title":           "Staff Engineer", // the current position, not the headline
		"current_company":     "Acme",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %#v, want %#v", k, m[k], v)
		}
	}
	exp := m["experience"].([]interface{})
	if len(exp) != 2 {
		t.Fatalf("experience = %#v", exp)
	}
	first := exp[0].(map[string]interface{})
	if first["employment_type"] != "Full-time" || first["location_type"] != "Hybrid" || first["company_url"] != "https://www.linkedin.com/company/acme-test/" {
		t.Errorf("experience[0] = %#v", first)
	}
	edu := m["education"].([]interface{})
	if len(edu) != 1 || edu[0].(map[string]interface{})["field_of_study"] != "Computer Science" || edu[0].(map[string]interface{})["grade"] != "1.3" {
		t.Errorf("education = %#v", edu)
	}
	if p.hitCount(hsScrollAbout) != 0 {
		t.Error("About was present, but the bot scrolled for it")
	}
}

func TestHealthGetProfileDataLoadsAboutAndFallsBackToPageURL(t *testing.T) {
	healthTimings(t)
	var base map[string]interface{}
	if err := json.Unmarshal(healthFixture(t, "profile"), &base); err != nil {
		t.Fatal(err)
	}
	base["url"] = ""
	p := newHealthPage(t).scriptCommon().
		on(hsProfile, func(c healthCall) interface{} {
			m := map[string]interface{}{}
			for k, v := range base {
				m[k] = v
			}
			if c.N < 2 { // first read and first re-read: About not rendered yet
				m["about"], m["aboutSection"] = "", false
			}
			return m
		}).
		onValue(hsScrollAbout, false).
		onValue(hsSection, map[string]interface{}{"found": false})
	_ = p.Navigate("https://www.linkedin.com/in/ann-lee-test/?trk=feed")
	m, err := (&LinkedInBot{}).GetProfileData(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if m["about"] != "Builds reliable things." {
		t.Errorf("about = %#v", m["about"])
	}
	if got := p.hitCount(hsScrollAbout); got != 2 {
		t.Errorf("scrolled for About %d times, want 2", got)
	}
	if m["profile_url"] != "https://www.linkedin.com/in/ann-lee-test/" || m["username"] != "ann-lee-test" {
		t.Errorf("profile_url/username from page URL = %v / %v", m["profile_url"], m["username"])
	}
	// No sections on the page: empty lists and the headline as job title.
	if m["job_title"] != "Staff Engineer at Acme" || len(m["experience"].([]interface{})) != 0 {
		t.Errorf("job_title=%v experience=%v", m["job_title"], m["experience"])
	}
}

func TestHealthLoadAboutStopsOnEmptySection(t *testing.T) {
	healthTimings(t)
	p := newHealthPage(t).scriptCommon().
		onValue(hsProfile, map[string]interface{}{"name": "Ann Lee", "about": "", "url": "https://www.linkedin.com/in/ann/"}).
		onValue(hsScrollAbout, true).
		onValue(hsSection, map[string]interface{}{"found": false})
	m, err := (&LinkedInBot{}).GetProfileData(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if m["about"] != "" {
		t.Errorf("about = %#v", m["about"])
	}
	// The section is there but empty: give up on the second look, not
	// after all aboutScrolls.
	if got := p.hitCount(hsScrollAbout); got != 2 {
		t.Errorf("scrolled %d times, want 2", got)
	}
}

func TestHealthGetProfileDataNoName(t *testing.T) {
	healthTimings(t)
	p := newHealthPage(t).onValue(hsProfile, map[string]interface{}{"name": ""})
	_, err := (&LinkedInBot{}).GetProfileData(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "no profile name") {
		t.Fatalf("err = %v", err)
	}
	if p.hitCount(hsProfile) < 2 {
		t.Error("the profile was not polled")
	}
}

func TestHealthGetProfileReadsDetailsPages(t *testing.T) {
	healthTimings(t)
	p := healthProfilePage(t, map[string]interface{}{
		"experience": healthFixture(t, "details_experience"),
		"education":  map[string]interface{}{"found": true, "entries": []interface{}{}}, // a member without education
	})
	fn, _ := (&LinkedInBot{}).GetMethodByName("get_profile_data")
	res, err := fn(context.Background(), p, "@ann-lee-test")
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	navs := p.navigations()
	wantNavs := []string{
		"https://www.linkedin.com/in/ann-lee-test/",
		"https://www.linkedin.com/in/ann-lee-test/details/experience/",
		"https://www.linkedin.com/in/ann-lee-test/details/education/",
	}
	if strings.Join(navs, "\n") != strings.Join(wantNavs, "\n") {
		t.Errorf("navigations = %v", navs)
	}
	var exp []Position
	b, _ := json.Marshal(m["experience"])
	_ = json.Unmarshal(b, &exp)
	if len(exp) != 3 {
		t.Fatalf("experience from details = %+v", exp)
	}
	if exp[0].Title != "Staff Engineer" || exp[0].Company != "Acme" || exp[0].EmploymentType != "Full-time" || exp[0].Location != "Berlin, Germany" || exp[0].Description != "Leads the platform team." {
		t.Errorf("grouped role 1 = %+v", exp[0])
	}
	if exp[1].Title != "Senior Engineer" || exp[1].Company != "Acme" || exp[1].End != "Dec 2021" {
		t.Errorf("grouped role 2 = %+v", exp[1])
	}
	if exp[2].Company != "Beta Corp" || exp[2].EmploymentType != "Contract" {
		t.Errorf("single role = %+v", exp[2])
	}
	if edu := m["education"].([]interface{}); len(edu) != 0 {
		t.Errorf("education from an empty details page = %#v (the profile page's entry must not leak in)", edu)
	}
	if m["job_title"] != "Staff Engineer" || m["current_company"] != "Acme" {
		t.Errorf("job_title/current_company = %v / %v", m["job_title"], m["current_company"])
	}
}

func TestHealthGetProfileFallsBackWhenDetailsNeverRender(t *testing.T) {
	healthTimings(t)
	p := healthProfilePage(t, nil)
	m, err := (&LinkedInBot{}).GetProfile(context.Background(), p, "https://www.linkedin.com/in/ann-lee-test/")
	if err != nil {
		t.Fatal(err)
	}
	exp := m["experience"].([]interface{})
	edu := m["education"].([]interface{})
	if len(exp) != 2 || len(edu) != 1 {
		t.Fatalf("fallback lists: experience=%d education=%d", len(exp), len(edu))
	}
	if exp[0].(map[string]interface{})["title"] != "Staff Engineer" {
		t.Errorf("experience[0] = %#v", exp[0])
	}
}

func TestHealthGetProfileErrors(t *testing.T) {
	healthTimings(t)
	b := &LinkedInBot{}
	if _, err := b.GetProfile(context.Background(), newHealthPage(t), "  "); err == nil {
		t.Error("blank target accepted")
	}
	p := newHealthPage(t)
	p.navErr = errors.New("tab closed")
	if _, err := b.GetProfile(context.Background(), p, "ann"); err == nil || !strings.Contains(err.Error(), "tab closed") {
		t.Errorf("navigation error = %v", err)
	}
	// Without details only the profile page is read.
	p2 := healthProfilePage(t, nil)
	fn, _ := b.GetMethodByName("get_profile_data")
	if _, err := fn(context.Background(), p2, "ann-lee-test", "false"); err != nil {
		t.Fatal(err)
	}
	if navs := p2.navigations(); len(navs) != 1 {
		t.Errorf("includeDetails=false navigated %v", navs)
	}
}

func TestHealthSearchPeople(t *testing.T) {
	healthTimings(t)
	p := newHealthPage(t).scriptCommon().on(hsPeople, func(c healthCall) interface{} {
		switch {
		case strings.Contains(c.URL, "page=2"):
			return healthFixture(t, "people_page2")
		case strings.Contains(c.URL, "page="):
			return []interface{}{}
		}
		return healthFixture(t, "people_page1")
	})
	fn, _ := (&LinkedInBot{}).GetMethodByName("search_people")
	res, err := fn(context.Background(), p, "staff engineer", "3")
	if err != nil {
		t.Fatal(err)
	}
	people := res.([]map[string]interface{})
	var names []string
	for _, x := range people {
		names = append(names, x["full_name"].(string))
	}
	// Page 1 gives two people (duplicate and link-less cards dropped); page
	// 2 adds one new person (Bo Kim again is skipped) up to max 3.
	if strings.Join(names, ",") != "Ann Lee,Bo Kim,Cy Diaz" {
		t.Errorf("people = %v", names)
	}
	if people[0]["username"] != "ann-lee-test" || people[0]["headline"] != "Staff Engineer at Acme" || people[0]["location"] != "Berlin" {
		t.Errorf("people[0] = %#v", people[0])
	}
	navs := p.navigations()
	if len(navs) != 2 || navs[0] != "https://www.linkedin.com/search/results/people/?keywords=staff+engineer&origin=SWITCH_SEARCH_VERTICAL" ||
		!strings.Contains(navs[1], "page=2") || !strings.Contains(navs[1], "keywords=staff+engineer") {
		t.Errorf("navigations = %v", navs)
	}
}

func TestHealthSearchPeopleStopsWhenAPageAddsNothing(t *testing.T) {
	healthTimings(t)
	p := newHealthPage(t).scriptCommon().onValue(hsPeople, healthFixture(t, "people_page1"))
	u := "https://www.linkedin.com/search/results/people/?keywords=x&network=%5B%22F%22%5D"
	people, err := (&LinkedInBot{}).SearchPeople(context.Background(), p, u, 0) // 0 ⇒ default 10
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Errorf("got %d people, want the 2 distinct ones", len(people))
	}
	navs := p.navigations()
	if len(navs) != 2 || navs[0] != u {
		t.Errorf("a search URL must be used as is and paging stop on a repeat page: %v", navs)
	}
}

func TestHealthSearchPeopleErrors(t *testing.T) {
	healthTimings(t)
	b := &LinkedInBot{}
	if _, err := b.SearchPeople(context.Background(), newHealthPage(t), "https://www.linkedin.com/search/results/content/?keywords=x", 5); err == nil || !strings.Contains(err.Error(), "not a people search URL") {
		t.Errorf("content search URL: %v", err)
	}
	if _, err := b.SearchPeople(context.Background(), newHealthPage(t), " ", 5); err == nil {
		t.Error("blank keyword accepted")
	}
	p := newHealthPage(t)
	p.navErr = errors.New("offline")
	if _, err := b.SearchPeople(context.Background(), p, "x", 5); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("navigation error = %v", err)
	}
	// No cards at all: an empty, non-nil result.
	empty := newHealthPage(t).onValue(hsPeople, []interface{}{})
	got, err := b.SearchPeople(context.Background(), empty, "nobody", 5)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("no results = %#v, %v", got, err)
	}
}

func TestHealthListFollowers(t *testing.T) {
	healthTimings(t)
	p := newHealthPage(t).scriptCommon().
		onValue(hsPeople, healthFixture(t, "people_page1")).
		onValue(hsListEmpty, false)
	fn, _ := (&LinkedInBot{}).GetMethodByName("list_followers")
	res, err := fn(context.Background(), p, "FOLLOWING_FETCH", "")
	if err != nil {
		t.Fatal(err)
	}
	people := res.([]map[string]interface{})
	if len(people) != 2 {
		t.Fatalf("followers = %#v", people)
	}
	if people[0]["you_follow"] != true || people[1]["you_follow"] != false {
		t.Errorf("you_follow = %v, %v", people[0]["you_follow"], people[1]["you_follow"])
	}
	if navs := p.navigations(); len(navs) != 1 || !strings.HasSuffix(navs[0], "/people-follow/following/") {
		t.Errorf("navigations = %v", navs)
	}
	// Three reads in a row add nothing new before giving up.
	if got := p.hitCount(hsPeople); got < 4 {
		t.Errorf("read the list %d times", got)
	}

	capped := newHealthPage(t).scriptCommon().onValue(hsPeople, healthFixture(t, "people_page1"))
	got, err := (&LinkedInBot{}).ListFollowers(context.Background(), capped, "", 1)
	if err != nil || len(got) != 1 || got[0]["full_name"] != "Ann Lee" {
		t.Errorf("max 1 = %#v, %v", got, err)
	}
	if navs := capped.navigations(); !strings.HasSuffix(navs[0], "/people-follow/followers/") {
		t.Errorf("default source = %v", navs)
	}
}

func TestHealthListFollowersEmptyAndNeverLoaded(t *testing.T) {
	healthTimings(t)
	b := &LinkedInBot{}
	empty := newHealthPage(t).scriptCommon().onValue(hsPeople, []interface{}{}).onValue(hsListEmpty, true)
	got, err := b.ListFollowers(context.Background(), empty, "followers", 10)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty state = %#v, %v", got, err)
	}

	blank := newHealthPage(t).scriptCommon().onValue(hsPeople, []interface{}{}).onValue(hsListEmpty, false)
	_, err = b.ListFollowers(context.Background(), blank, "following", 10)
	if err == nil || !strings.Contains(err.Error(), "the following list didn't load") {
		t.Errorf("never loaded = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.ListFollowers(ctx, blank, "followers", 10); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled = %v", err)
	}
}
