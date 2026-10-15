package extension

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"
)

// form.card and form.photos — the two things Fill form never does.
//
//	form.card   {profile, fields:[{id,label,name,type,autocomplete,placeholder,maxlength,options?}]}
//	  reply     {values:[{id,value}], saved:bool}
//	form.photos {profile, fields:[{id,label,name,accept}]}
//	  reply     {files:[{id,key,name,mime,size,data}]}   data is base64
//
// The side panel has a button for each, so the person asks for them one at a
// time. Neither goes near a model: a card field is recognised by its
// autocomplete token or its name, a file input by its label, and the answer is
// whatever the person stored on the Info page. Only the card the person typed
// on the Info page is ever used; there is nothing to fill if they did not.

// Request methods.
const (
	MethodFormCard   = "form.card"
	MethodFormPhotos = "form.photos"
)

// CardDetails is the card the person stored.
type CardDetails struct {
	Holder, Number, Expiry, CVV string
}

// PhotoFile is one stored photo or scan.
type PhotoFile struct {
	Name string
	Mime string
	Data []byte
}

// FormAssets is the optional second half of a FormFiller: if the filler also
// implements it, form.card and form.photos are served.
type FormAssets interface {
	// Card returns the stored card; ok is false when none is saved.
	Card(ctx context.Context, profile string) (card CardDetails, ok bool, err error)
	// Photo returns the stored photo for an Info photo key; ok is false when none.
	Photo(ctx context.Context, profile, key string) (file PhotoFile, ok bool, err error)
}

const (
	formMaxPhotoFields = 12
	formMaxPhotoBytes  = 10 << 20
	formMaxPhotoTotal  = 20 << 20
)

func (s *Server) installFormAssets(a FormAssets) {
	s.HandleRequestWithTimeout(MethodFormCard, func(ctx context.Context, req *Request, _ ProgressFunc) (any, error) {
		return handleFormCard(ctx, a, req)
	}, chatQueryTimeout)
	s.HandleRequestWithTimeout(MethodFormPhotos, func(ctx context.Context, req *Request, _ ProgressFunc) (any, error) {
		return handleFormPhotos(ctx, a, req)
	}, formFillTimeout)
}

func (s *Server) removeFormAssets() {
	s.handlerMu.Lock()
	delete(s.handlers, MethodFormCard)
	delete(s.handlers, MethodFormPhotos)
	delete(s.methodTimeouts, MethodFormCard)
	delete(s.methodTimeouts, MethodFormPhotos)
	s.handlerMu.Unlock()
}

// ── card ─────────────────────────────────────────────────────────────

// CardValuesReply is form.card's reply.
type CardValuesReply struct {
	Values []FormValue `json:"values"`
	Saved  bool        `json:"saved"`
}

var (
	cardNumberRe = regexp.MustCompile(`(?i)card.?(number|no\b|num\b)|credit.?card|debit.?card|\bpan\b`)
	cardCVVRe    = regexp.MustCompile(`(?i)\bcvv2?\b|\bcvc2?\b|\bcsc\b|security.?code|card.?verification`)
	cardHolderRe = regexp.MustCompile(`(?i)name.?on.?card|card.?holder|cardholder`)
	cardExpiryRe = regexp.MustCompile(`(?i)expir|\bexp\b|valid.?(thru|through|until)`)
	expiryParse  = regexp.MustCompile(`^\s*(\d{1,2})\s*[/\-.]?\s*(\d{2}|\d{4})\s*$`)
)

// cardRole is what a card field asks for.
type cardRole int

const (
	roleNone cardRole = iota
	roleHolder
	roleNumber
	roleExpiry      // month and year together
	roleExpiryMonth // month alone
	roleExpiryYear  // year alone
	roleCVV
)

func cardRoleOf(f FormField) cardRole {
	switch lastToken(f.Autocomplete) {
	case "cc-name":
		return roleHolder
	case "cc-number":
		return roleNumber
	case "cc-exp":
		return roleExpiry
	case "cc-exp-month":
		return roleExpiryMonth
	case "cc-exp-year":
		return roleExpiryYear
	case "cc-csc":
		return roleCVV
	}
	hay := f.Name + " " + f.Label + " " + f.Placeholder
	switch {
	case cardCVVRe.MatchString(hay):
		return roleCVV
	case cardNumberRe.MatchString(hay):
		return roleNumber
	case cardHolderRe.MatchString(hay):
		return roleHolder
	case cardExpiryRe.MatchString(hay):
		l := strings.ToLower(hay)
		switch {
		case strings.Contains(l, "month") && !strings.Contains(l, "year"):
			return roleExpiryMonth
		case strings.Contains(l, "year") && !strings.Contains(l, "month"):
			return roleExpiryYear
		}
		return roleExpiry
	}
	return roleNone
}

// splitExpiry reads "MM/YY", "MM/YYYY", "0127" and the like.
func splitExpiry(s string) (month, year string, ok bool) {
	m := expiryParse.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	month = m[1]
	if len(month) == 1 {
		month = "0" + month
	}
	if month < "01" || month > "12" {
		return "", "", false
	}
	return month, m[2], true
}

func fullYear(y string) string {
	if len(y) == 2 {
		return "20" + y
	}
	return y
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cardValue is the text for field f playing role, or "" if the card has none
// for it or the field cannot take it.
func cardValue(f FormField, role cardRole, c CardDetails) string {
	month, year, expOK := splitExpiry(c.Expiry)
	var v string
	switch role {
	case roleHolder:
		v = c.Holder
	case roleNumber:
		v = digits(c.Number)
	case roleCVV:
		v = digits(c.CVV)
	case roleExpiryMonth:
		if expOK {
			v = month
		}
	case roleExpiryYear:
		if expOK {
			// A two-digit box (or a list of two-digit years) wants YY.
			v = fullYear(year)
			if f.MaxLength == 2 || (f.isChoice() && !hasOptionContaining(f.Options, v)) {
				v = v[len(v)-2:]
			}
		}
	case roleExpiry:
		if expOK {
			hint := strings.ToUpper(f.Placeholder + " " + f.Label)
			switch {
			case f.MaxLength == 4:
				v = month + year[len(year)-2:]
			case strings.Contains(hint, "YYYY"):
				v = month + "/" + fullYear(year)
			default:
				v = month + "/" + year[len(year)-2:]
			}
		}
	}
	if v == "" {
		return ""
	}
	if f.isChoice() {
		o, ok := optionFor(f.Options, v)
		if !ok {
			return ""
		}
		return o
	}
	return v
}

func hasOptionContaining(options []string, s string) bool {
	for _, o := range options {
		if strings.Contains(o, s) {
			return true
		}
	}
	return false
}

func handleFormCard(ctx context.Context, a FormAssets, req *Request) (any, error) {
	profile, err := chatProfile(req)
	if err != nil {
		return nil, err
	}
	if profile == "" {
		return nil, invalidParam("form.card needs a profile")
	}
	fields, _, err := parseFormFields(req, true)
	if err != nil {
		return nil, err
	}
	card, ok, err := a.Card(ctx, profile)
	if err != nil {
		return nil, Unavailable("cannot read the saved card: %v", err)
	}
	reply := &CardValuesReply{Values: []FormValue{}, Saved: ok}
	if !ok {
		return reply, nil
	}
	for _, f := range fields {
		if role := cardRoleOf(f); role != roleNone {
			if v := cardValue(f, role, card); v != "" {
				reply.Values = append(reply.Values, FormValue{ID: f.ID, Value: v, Source: InfoSource})
			}
		}
	}
	return reply, nil
}

// ── photos ───────────────────────────────────────────────────────────

// PhotoReply is form.photos' reply.
type PhotoReply struct {
	Files []PhotoReplyFile `json:"files"`
}

// PhotoReplyFile is one file for one file input.
type PhotoReplyFile struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int    `json:"size"`
	Data string `json:"data"`
}

// photoRules map what a file input's label or name says to an Info photo key,
// most specific first.
var photoRules = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`(?i)proof.?of.?(address|residence)|utility.?bill|bank.?statement`), "proof_of_address_photo"},
	{regexp.MustCompile(`(?i)signature`), "signature_photo"},
	{regexp.MustCompile(`(?i)passport`), "passport_photo"},
	{regexp.MustCompile(`(?i)driv(er|ing)'?s?.?licen[cs]e`), "drivers_license_photo"},
	{regexp.MustCompile(`(?i)(id|identity|national).?(card)?.{0,12}back|back.{0,12}(of)?.{0,8}(id|identity|card)`), "id_back_photo"},
	{regexp.MustCompile(`(?i)(id|identity|national).?card|identity.?document|\bid\b.{0,12}(front|photo|scan|copy)|front.{0,12}(of)?.{0,8}(id|identity|card)`), "id_front_photo"},
	{regexp.MustCompile(`(?i)portrait|selfie|headshot|profile.?(photo|picture|image)|passport.?size|your.?(photo|picture)|\bavatar\b|\bphoto\b|\bpicture\b`), "portrait_photo"},
}

func photoKeyFor(f FormField) string {
	hay := f.Label + " " + f.Name + " " + f.Placeholder
	for _, r := range photoRules {
		if r.re.MatchString(hay) {
			return r.key
		}
	}
	return ""
}

// acceptsFile reports whether a file input's accept list allows the file.
func acceptsFile(accept string, p PhotoFile) bool {
	accept = strings.TrimSpace(strings.ToLower(accept))
	if accept == "" {
		return true
	}
	name := strings.ToLower(p.Name)
	for _, tok := range strings.Split(accept, ",") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "":
		case tok == p.Mime, tok == "*/*":
			return true
		case strings.HasSuffix(tok, "/*") && strings.HasPrefix(p.Mime, strings.TrimSuffix(tok, "*")):
			return true
		case strings.HasPrefix(tok, ".") && strings.HasSuffix(name, tok):
			return true
		}
	}
	return false
}

func handleFormPhotos(ctx context.Context, a FormAssets, req *Request) (any, error) {
	profile, err := chatProfile(req)
	if err != nil {
		return nil, err
	}
	if profile == "" {
		return nil, invalidParam("form.photos needs a profile")
	}
	raw, ok := req.Params["fields"].([]any)
	if !ok || len(raw) == 0 {
		return nil, invalidParam("form.photos needs fields")
	}
	if len(raw) > formMaxPhotoFields {
		raw = raw[:formMaxPhotoFields]
	}
	reply := &PhotoReply{Files: []PhotoReplyFile{}}
	total := 0
	cache := map[string]*PhotoFile{}
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, invalidParam("fields must be objects")
		}
		str := func(k string, max int) string {
			s, _ := m[k].(string)
			return truncateUTF8(plainField(s, false), max)
		}
		f := FormField{ID: str("id", 64), Label: str("label", formMaxLabel), Name: str("name", 100), Placeholder: str("placeholder", formMaxLabel)}
		if !formFieldID.MatchString(f.ID) {
			return nil, invalidParam("every field needs an id")
		}
		key := photoKeyFor(f)
		if key == "" {
			continue
		}
		p, seen := cache[key]
		if !seen {
			file, ok, err := a.Photo(ctx, profile, key)
			if err != nil {
				return nil, Unavailable("cannot read the saved photo: %v", err)
			}
			if ok && len(file.Data) <= formMaxPhotoBytes {
				p = &file
			}
			cache[key] = p
		}
		if p == nil || !acceptsFile(str("accept", 200), *p) || total+len(p.Data) > formMaxPhotoTotal {
			continue
		}
		total += len(p.Data)
		reply.Files = append(reply.Files, PhotoReplyFile{
			ID: f.ID, Key: key, Name: p.Name, Mime: p.Mime, Size: len(p.Data),
			Data: base64.StdEncoding.EncodeToString(p.Data),
		})
	}
	return reply, nil
}
