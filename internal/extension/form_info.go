package extension

import (
	"strings"
	"unicode"
)

// Matching a form's fields to the profile's Info (the personal details the
// person keeps on the Info page) without a model.
//
// A form field is matched in this order: the HTML autocomplete token it
// carries, its name, its label, then its input type (email, tel). A field with
// a match is filled with the Info value as it is; no model sees it. That is
// what keeps ID and bank numbers out of any model: they are only ever filled
// here, never offered as facts (see modelFacts).

// InfoSource labels a value that came from the Info page.
const InfoSource = "Profile info"

// InfoValue is one Info field that has a value. The bridge's backend leaves
// out photos and the payment card.
type InfoValue struct {
	Key          string
	Label        string
	Kind         string // text, textarea, date, select, email, phone, country
	Autocomplete string
	Sensitive    bool
	Value        string
}

// infoAliases maps what forms call a field (letters and digits only, lower
// case) to the Info key.
var infoAliases = map[string]string{
	"firstname": "first_name", "givenname": "first_name", "fname": "first_name", "forename": "first_name",
	"lastname": "last_name", "surname": "last_name", "familyname": "last_name", "lname": "last_name",
	"middlename": "middle_name", "middlenames": "middle_name", "middleinitial": "middle_name",
	"fullname": "full_name", "name": "full_name", "yourname": "full_name",
	"email": "email", "emailaddress": "email", "mail": "email", "youremail": "email",
	"phone": "phone", "phonenumber": "phone", "mobile": "phone", "mobilephone": "phone", "mobilenumber": "phone",
	"telephone": "phone", "tel": "phone", "cell": "phone", "cellphone": "phone",
	"phonetype": "phone_type", "countrycode": "phone_country_code",
	"address": "address_line1", "addressline1": "address_line1", "address1": "address_line1",
	"street": "address_line1", "streetaddress": "address_line1", "addressline2": "address_line2", "address2": "address_line2",
	"city": "city", "town": "city", "state": "state", "province": "state", "region": "state", "stateprovince": "state",
	"zip": "postal_code", "zipcode": "postal_code", "postcode": "postal_code", "postalcode": "postal_code", "zippostalcode": "postal_code",
	"country": "country", "countryofresidence": "country",
	"dob": "date_of_birth", "dateofbirth": "date_of_birth", "birthdate": "date_of_birth", "birthday": "date_of_birth",
	"placeofbirth": "place_of_birth", "countryofbirth": "country_of_birth",
	"title": "title", "salutation": "title", "gender": "gender", "sex": "gender",
	"nationality": "nationality", "citizenship": "nationality", "maritalstatus": "marital_status",
	"company": "employer", "employer": "employer", "employername": "employer", "organization": "employer", "organisation": "employer",
	"jobtitle": "job_title", "occupation": "job_title", "position": "job_title",
	"website": "website", "url": "website", "linkedin": "linkedin", "github": "github",
	"passport": "passport_number", "passportnumber": "passport_number", "taxid": "tax_id", "tin": "tax_id",
	"companyname": "company_name", "businessname": "company_name", "legalname": "company_name", "organizationname": "company_name",
	"vat": "vat_id", "vatid": "vat_id", "vatnumber": "vat_id", "vatno": "vat_id", "vatregistrationnumber": "vat_id",
	"companynumber": "company_registration", "registrationnumber": "company_registration", "companyregistrationnumber": "company_registration",
	"crn": "company_registration", "tradingname": "trading_name", "dba": "trading_name",
	"eori": "eori_number", "duns": "duns", "dunsnumber": "duns", "lei": "lei", "giin": "giin",
	"iban": "iban", "bic": "bic", "swift": "bic", "swiftcode": "bic",
}

// autocompleteKeys maps HTML autocomplete tokens the catalog does not carry.
var autocompleteKeys = map[string]string{
	"street-address": "address_line1", "tel-national": "phone", "tel-local": "phone",
	"country": "country", "country-name": "country", "name": "full_name",
}

func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lastToken is the field-name part of an autocomplete value: "billing
// given-name" is "given-name".
func lastToken(s string) string {
	f := strings.Fields(strings.ToLower(s))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// optionFor picks the option for value: the same text, else the one option
// that contains it or is contained by it ("Germany" for "DE - Germany").
func optionFor(options []string, value string) (string, bool) {
	if o, ok := matchOption(options, value); ok {
		return o, true
	}
	v := strings.ToLower(strings.TrimSpace(value))
	if len(v) < 3 {
		return "", false
	}
	found := ""
	for _, o := range options {
		lo := strings.ToLower(o)
		if len(lo) >= 3 && (strings.Contains(lo, v) || strings.Contains(v, lo)) {
			if found != "" {
				return "", false // two options fit: not exact
			}
			found = o
		}
	}
	return found, found != ""
}

// matchInfo fills the fields the Info answers exactly, and returns them with
// the fields it did not answer, both in the form's order.
func matchInfo(fields []FormField, info []InfoValue) (values []FormValue, rest []FormField) {
	if len(info) == 0 {
		return nil, fields
	}
	byKey := make(map[string]InfoValue, len(info))
	byAuto := map[string]string{}
	for _, v := range info {
		byKey[v.Key] = v
		if v.Autocomplete != "" {
			byAuto[v.Autocomplete] = v.Key
		}
	}
	index := map[string]string{}
	for _, v := range info {
		index[normName(v.Key)] = v.Key
		index[normName(v.Label)] = v.Key
	}
	for alias, key := range infoAliases {
		if _, ok := byKey[key]; ok {
			index[alias] = key
		}
	}
	for _, f := range fields {
		var candidates []string
		if tok := lastToken(f.Autocomplete); tok != "" {
			if k, ok := byAuto[tok]; ok {
				candidates = append(candidates, k)
			} else if k, ok := autocompleteKeys[tok]; ok {
				candidates = append(candidates, k)
			}
		}
		for _, s := range []string{f.Name, f.Label} {
			if k, ok := index[normName(s)]; ok && normName(s) != "" {
				candidates = append(candidates, k)
			}
		}
		switch f.Type {
		case "email":
			candidates = append(candidates, "email")
		case "tel":
			candidates = append(candidates, "phone")
		}
		if v, ok := exactValue(f, candidates, byKey); ok {
			values = append(values, FormValue{ID: f.ID, Value: v, Source: InfoSource})
		} else {
			rest = append(rest, f)
		}
	}
	return values, rest
}

// exactValue is the first candidate's value that fits the field as it is.
func exactValue(f FormField, candidates []string, byKey map[string]InfoValue) (string, bool) {
	for _, k := range candidates {
		v, ok := byKey[k]
		if !ok {
			continue
		}
		// A date in a text box may need another layout (DD/MM/YYYY): that is
		// for the model, which sees the label and placeholder.
		if (v.Kind == "date") != (f.Type == "date") {
			continue
		}
		if f.isChoice() {
			if o, ok := optionFor(f.Options, v.Value); ok {
				return o, true
			}
			continue
		}
		max := formMaxText
		if f.Type == "textarea" {
			max = formMaxTextarea
		}
		if len(v.Value) <= max {
			return v.Value, true
		}
	}
	return "", false
}

// modelFacts is the Info a model may see: everything but ID and bank numbers.
// The card never reaches the bridge's Info at all.
func modelFacts(info []InfoValue) []FormFact {
	var out []FormFact
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, FormFact{Source: InfoSource, Text: strings.TrimSpace(b.String())})
			b.Reset()
		}
	}
	for _, v := range info {
		if v.Sensitive {
			continue
		}
		line := v.Label + ": " + strings.ReplaceAll(v.Value, "\n", " ") + "\n"
		if b.Len()+len(line) > formMaxFactText {
			flush()
		}
		b.WriteString(line)
	}
	flush()
	return out
}
