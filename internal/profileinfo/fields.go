// Package profileinfo is the personal details a profile holds for filling web
// forms: a catalog of fields (this file), their storage (store.go), and the
// AI step that fills the empty ones from the profile's documents (aifill.go).
//
// The catalog is the one list of fields. The CLI prints it
// (`profile info fields --json`), the desktop page and the extension render and
// match from it, so a field added here appears everywhere.
package profileinfo

// Kind says how a field is shown and checked.
type Kind string

const (
	Text     Kind = "text"     // one line
	Textarea Kind = "textarea" // several lines
	Date     Kind = "date"     // YYYY-MM-DD
	Select   Kind = "select"   // one of Options
	Email    Kind = "email"
	Phone    Kind = "phone"
	Country  Kind = "country" // a country name
	Photo    Kind = "photo"   // an image or PDF kept in the profile's folder
)

// Field is one piece of information.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
	Kind  Kind   `json:"kind"`
	// Options is the choices of a Select.
	Options []string `json:"options,omitempty"`
	// Sensitive values (ID and bank numbers) are stored encrypted and shown
	// masked until asked for.
	Sensitive bool `json:"sensitive,omitempty"`
	// Card values are stored encrypted like Sensitive ones, and are never
	// extracted by the AI nor sent to a model: only the extension fills them,
	// by exact field match and on its own button.
	Card bool `json:"card,omitempty"`
	// Autocomplete is the HTML autocomplete token a form field for this info
	// usually carries; the extension fills by it before it asks a model.
	Autocomplete string `json:"autocomplete,omitempty"`
}

// Group is a section of the page, in display order.
type Group struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Groups in display order.
var Groups = []Group{
	{"identity", "Identity"}, {"contact", "Contact"}, {"address", "Home address"},
	{"mailing", "Mailing & previous address"}, {"ids", "Government IDs"},
	{"work", "Work"}, {"education", "Education"}, {"online", "Online"},
	{"financial", "Bank & business"}, {"compliance", "Investing & compliance"},
	{"emergency", "Emergency contact & family"}, {"about", "About you"},
	{"photos", "Photos & scans"}, {"card", "Payment card"},
}

var (
	yesNo   = []string{"Yes", "No"}
	titles  = []string{"Mr.", "Ms.", "Mrs.", "Miss", "Mx.", "Dr.", "Prof."}
	genders = []string{"Male", "Female", "Non-binary", "Prefer not to say"}
	marital = []string{"Single", "Married", "Civil partnership", "Divorced", "Widowed", "Separated"}
	empl    = []string{"Employed", "Self-employed", "Unemployed", "Student", "Retired", "Homemaker"}
	degrees = []string{"High school", "Associate", "Bachelor", "Master", "Doctorate", "Vocational", "Other"}
	ranges  = []string{"Under 25,000", "25,000 - 50,000", "50,000 - 100,000", "100,000 - 250,000", "250,000 - 1,000,000", "Over 1,000,000"}
	exper   = []string{"None", "Limited", "Good", "Extensive"}
	goals   = []string{"Capital preservation", "Income", "Growth", "Speculation"}
	phoneTy = []string{"Mobile", "Home", "Work"}
)

func f(group, key, label string, kind Kind, ac string) Field {
	return Field{Key: key, Label: label, Group: group, Kind: kind, Autocomplete: ac}
}

func sel(group, key, label string, opts []string) Field {
	return Field{Key: key, Label: label, Group: group, Kind: Select, Options: opts}
}

func sens(x Field) Field { x.Sensitive = true; return x }
func card(x Field) Field { x.Card = true; return x }

// Catalog is every field, in display order.
var Catalog = []Field{
	// Identity
	sel("identity", "title", "Title", titles),
	f("identity", "first_name", "First name", Text, "given-name"),
	f("identity", "middle_name", "Middle name(s)", Text, "additional-name"),
	f("identity", "last_name", "Last name", Text, "family-name"),
	f("identity", "full_name", "Full name", Text, "name"),
	f("identity", "previous_names", "Previous or maiden names", Text, ""),
	f("identity", "nickname", "Nickname", Text, "nickname"),
	sel("identity", "gender", "Gender", genders),
	f("identity", "date_of_birth", "Date of birth", Date, "bday"),
	f("identity", "place_of_birth", "Place of birth", Text, ""),
	f("identity", "country_of_birth", "Country of birth", Country, ""),
	f("identity", "nationality", "Nationality", Country, ""),
	f("identity", "other_nationalities", "Other nationalities", Text, ""),
	sel("identity", "marital_status", "Marital status", marital),
	f("identity", "mother_name", "Mother's full name", Text, ""),
	f("identity", "father_name", "Father's full name", Text, ""),
	f("identity", "preferred_language", "Preferred language", Text, "language"),
	// Contact
	f("contact", "email", "Email", Email, "email"),
	f("contact", "email_secondary", "Second email", Email, ""),
	f("contact", "phone", "Mobile phone", Phone, "tel"),
	sel("contact", "phone_type", "Phone type", phoneTy),
	f("contact", "phone_country_code", "Phone country code", Text, "tel-country-code"),
	f("contact", "phone_home", "Home phone", Phone, ""),
	f("contact", "phone_work", "Work phone", Phone, ""),
	// Home address
	f("address", "address_line1", "Address line 1", Text, "address-line1"),
	f("address", "address_line2", "Address line 2", Text, "address-line2"),
	f("address", "city", "City", Text, "address-level2"),
	f("address", "state", "State / province / region", Text, "address-level1"),
	f("address", "postal_code", "Postal / ZIP code", Text, "postal-code"),
	f("address", "country", "Country", Country, "country-name"),
	f("address", "address_since", "Living here since", Date, ""),
	sel("address", "residency_status", "Residency status", []string{"Citizen", "Permanent resident", "Temporary resident", "Visa holder", "Other"}),
	// Mailing & previous
	f("mailing", "mailing_address", "Mailing address (if different)", Textarea, ""),
	f("mailing", "previous_address", "Previous address", Textarea, ""),
	// Government IDs
	sens(f("ids", "passport_number", "Passport number", Text, "")),
	f("ids", "passport_issue_date", "Passport issue date", Date, ""),
	f("ids", "passport_expiry_date", "Passport expiry date", Date, ""),
	f("ids", "passport_country", "Passport issuing country", Country, ""),
	sens(f("ids", "id_card_number", "National ID card number", Text, "")),
	f("ids", "id_card_expiry_date", "ID card expiry date", Date, ""),
	sens(f("ids", "drivers_license_number", "Driver's licence number", Text, "")),
	f("ids", "drivers_license_expiry_date", "Driver's licence expiry date", Date, ""),
	sens(f("ids", "tax_id", "Tax ID", Text, "")),
	f("ids", "tax_country", "Country of tax residence", Country, ""),
	sens(f("ids", "social_security_number", "Social security / national insurance number", Text, "")),
	sens(f("ids", "visa_number", "Visa or residence permit number", Text, "")),
	f("ids", "visa_expiry_date", "Visa or permit expiry date", Date, ""),
	// Work
	sel("work", "employment_status", "Employment status", empl),
	f("work", "employer", "Employer", Text, "organization"),
	f("work", "job_title", "Job title", Text, "organization-title"),
	f("work", "industry", "Industry", Text, ""),
	f("work", "employer_address", "Employer address", Textarea, ""),
	f("work", "work_email", "Work email", Email, ""),
	f("work", "employed_since", "Employed since", Date, ""),
	f("work", "years_experience", "Years of experience", Text, ""),
	sel("work", "annual_income", "Annual income", ranges),
	f("work", "source_of_funds", "Source of funds", Text, ""),
	// Education
	sel("education", "highest_degree", "Highest education", degrees),
	f("education", "school", "School or university", Text, ""),
	f("education", "field_of_study", "Field of study", Text, ""),
	f("education", "graduation_year", "Graduation year", Text, ""),
	f("education", "languages_spoken", "Languages spoken", Text, ""),
	// Online
	f("online", "website", "Website", Text, "url"),
	f("online", "linkedin", "LinkedIn", Text, ""),
	f("online", "github", "GitHub", Text, ""),
	f("online", "twitter", "X / Twitter", Text, ""),
	f("online", "username", "Preferred username", Text, "username"),
	// Bank & business
	f("financial", "bank_name", "Bank name", Text, ""),
	f("financial", "account_holder", "Account holder", Text, ""),
	sens(f("financial", "iban", "IBAN", Text, "")),
	sens(f("financial", "bic", "BIC / SWIFT", Text, "")),
	sens(f("financial", "bank_account_number", "Account number", Text, "")),
	sens(f("financial", "bank_routing_number", "Routing / sort code", Text, "")),
	f("financial", "company_name", "Company name", Text, ""),
	f("financial", "company_registration", "Company registration number", Text, ""),
	f("financial", "vat_id", "VAT ID", Text, ""),
	f("financial", "company_address", "Company address", Textarea, ""),
	// Investing & compliance
	sel("compliance", "investment_experience", "Investment experience", exper),
	sel("compliance", "investment_objective", "Investment objective", goals),
	sel("compliance", "net_worth", "Net worth", ranges),
	sel("compliance", "liquid_net_worth", "Liquid net worth", ranges),
	sel("compliance", "politically_exposed", "Politically exposed person", yesNo),
	sel("compliance", "us_person", "US person for tax purposes", yesNo),
	sel("compliance", "employed_in_finance", "Works in the financial industry", yesNo),
	// Emergency & family
	f("emergency", "emergency_name", "Emergency contact name", Text, ""),
	f("emergency", "emergency_relationship", "Relationship", Text, ""),
	f("emergency", "emergency_phone", "Emergency contact phone", Phone, ""),
	f("emergency", "emergency_email", "Emergency contact email", Email, ""),
	f("emergency", "spouse_name", "Spouse or partner", Text, ""),
	f("emergency", "dependants", "Number of dependants", Text, ""),
	// About
	f("about", "bio", "Short bio", Textarea, ""),
	f("about", "about_me", "About me (long)", Textarea, ""),
	f("about", "cv_summary", "CV summary", Textarea, ""),
	f("about", "cover_letter", "Cover letter text", Textarea, ""),
	f("about", "skills", "Skills", Textarea, ""),
	f("about", "notes", "Anything else forms ask for", Textarea, ""),
	// Photos & scans
	f("photos", "portrait_photo", "Portrait photo", Photo, ""),
	f("photos", "id_front_photo", "ID card, front", Photo, ""),
	f("photos", "id_back_photo", "ID card, back", Photo, ""),
	f("photos", "passport_photo", "Passport photo page", Photo, ""),
	f("photos", "drivers_license_photo", "Driver's licence", Photo, ""),
	f("photos", "proof_of_address_photo", "Proof of address", Photo, ""),
	f("photos", "signature_photo", "Signature", Photo, ""),
	// Payment card: stored only if the person types it here
	card(f("card", "card_holder", "Name on card", Text, "cc-name")),
	card(f("card", "card_number", "Card number", Text, "cc-number")),
	card(f("card", "card_expiry", "Expiry (MM/YY)", Text, "cc-exp")),
	card(f("card", "card_cvv", "Security code", Text, "cc-csc")),
	card(f("card", "card_billing_address", "Billing address", Textarea, "")),
}

var byKey = func() map[string]*Field {
	m := make(map[string]*Field, len(Catalog))
	for i := range Catalog {
		m[Catalog[i].Key] = &Catalog[i]
	}
	return m
}()

// Lookup returns the catalog field with key, or nil.
func Lookup(key string) *Field { return byKey[key] }

// Encrypted reports whether the field's value is stored encrypted.
func (x Field) Encrypted() bool { return x.Sensitive || x.Card }
