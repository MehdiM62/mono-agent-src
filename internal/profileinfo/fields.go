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

// A profile is either a person or a business; the Info page shows the groups
// of the one it is. Both kinds are kept when it is switched, only hidden.
const (
	ScopePerson   = "person"
	ScopeBusiness = "business"
)

// TypeKey is the field holding which of the two the profile is.
const TypeKey = "profile_type"

// Group is a section of the page, in display order. Scope says whose page it
// belongs to (ScopePerson, ScopeBusiness); empty means both.
type Group struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope,omitempty"`
}

// Groups in display order.
var Groups = []Group{
	{"type", "Profile type", ""},
	// A person
	{"identity", "Identity", ScopePerson}, {"contact", "Contact", ScopePerson}, {"address", "Home address", ScopePerson},
	{"mailing", "Mailing & previous address", ScopePerson}, {"ids", "Government IDs", ScopePerson},
	{"work", "Work", ScopePerson}, {"education", "Education", ScopePerson},
	{"compliance", "Investing & compliance", ScopePerson},
	{"emergency", "Emergency contact & family", ScopePerson}, {"about", "About you", ScopePerson},
	{"photos", "Photos & scans", ScopePerson},
	// A business
	{"biz_identity", "Business identity", ScopeBusiness}, {"biz_about", "About the business", ScopeBusiness},
	{"biz_tax", "Tax & registration", ScopeBusiness}, {"biz_contact", "Business contact", ScopeBusiness},
	{"biz_address", "Business addresses", ScopeBusiness}, {"biz_owners", "Owners, directors & signatories", ScopeBusiness},
	{"biz_finance", "Finance & activity", ScopeBusiness}, {"biz_compliance", "Regulation & compliance", ScopeBusiness},
	{"biz_docs", "Business documents", ScopeBusiness},
	// Either
	{"online", "Online", ""}, {"financial", "Bank account", ""}, {"card", "Payment card", ""},
}

var groupScope = func() map[string]string {
	m := make(map[string]string, len(Groups))
	for _, g := range Groups {
		m[g.ID] = g.Scope
	}
	return m
}()

// Scope is who the field is for: ScopePerson, ScopeBusiness, or "" for both.
func (x Field) Scope() string { return groupScope[x.Group] }

// InScope reports whether a profile of the given type (ScopePerson or
// ScopeBusiness) shows the field.
func (x Field) InScope(profileType string) bool {
	s := x.Scope()
	return s == "" || s == profileType
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

	legalForms  = []string{"Sole proprietorship", "Partnership", "Limited liability company (LLC, GmbH, Ltd)", "Corporation (Inc., AG, PLC)", "Non-profit or association", "Cooperative", "Other"}
	coStatus    = []string{"Active", "Dormant", "In liquidation", "Not yet formed"}
	headcount   = []string{"Just me", "2 - 10", "11 - 50", "51 - 250", "251 - 1,000", "Over 1,000"}
	revenue     = []string{"Under 100,000", "100,000 - 500,000", "500,000 - 2,000,000", "2,000,000 - 10,000,000", "10,000,000 - 50,000,000", "Over 50,000,000"}
	filing      = []string{"Monthly", "Quarterly", "Annually"}
	ownership   = []string{"Single owner", "Several owners", "Owned by another company", "Publicly listed", "State-owned", "Other"}
	customers   = []string{"Consumers (B2C)", "Businesses (B2B)", "Both", "Government"}
	crsEntities = []string{"Active non-financial entity", "Passive non-financial entity", "Financial institution", "Other"}
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
	// Profile type
	sel("type", TypeKey, "Profile type", []string{"Person", "Business"}),
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
	// Business identity
	f("biz_identity", "company_name", "Legal company name", Text, "organization"),
	f("biz_identity", "trading_name", "Trading name (DBA)", Text, ""),
	sel("biz_identity", "legal_form", "Legal form", legalForms),
	sens(f("biz_identity", "company_registration", "Company registration number", Text, "")),
	f("biz_identity", "registration_country", "Country of registration", Country, ""),
	f("biz_identity", "incorporation_date", "Date of incorporation", Date, ""),
	f("biz_identity", "registry_name", "Registry or court of registration", Text, ""),
	sens(f("biz_identity", "lei", "LEI (Legal Entity Identifier)", Text, "")),
	sens(f("biz_identity", "duns", "D-U-N-S number", Text, "")),
	sel("biz_identity", "company_status", "Company status", coStatus),
	f("biz_identity", "business_industry", "Industry", Text, ""),
	f("biz_identity", "industry_code", "Industry code (NAICS, SIC or NACE)", Text, ""),
	f("biz_identity", "fiscal_year_end", "Financial year end (MM-DD)", Text, ""),
	// About the business
	f("biz_about", "tagline", "Tagline", Text, ""),
	f("biz_about", "business_description", "What the business does", Textarea, ""),
	f("biz_about", "products_services", "Products and services", Textarea, ""),
	f("biz_about", "year_founded", "Year founded", Text, ""),
	sel("biz_about", "customer_type", "Customers", customers),
	f("biz_about", "company_boilerplate", "Company boilerplate (About us)", Textarea, ""),
	f("biz_about", "business_notes", "Anything else forms ask for", Textarea, ""),
	// Tax & registration
	sens(f("biz_tax", "vat_id", "VAT ID", Text, "")),
	sel("biz_tax", "vat_registered", "VAT registered", yesNo),
	sel("biz_tax", "vat_filing_frequency", "VAT filing frequency", filing),
	sens(f("biz_tax", "company_tax_id", "Business tax ID (EIN, TIN, Steuernummer)", Text, "")),
	f("biz_tax", "company_tax_country", "Country of tax residence", Country, ""),
	f("biz_tax", "tax_office", "Tax office", Text, ""),
	sens(f("biz_tax", "eori_number", "EORI number", Text, "")),
	sens(f("biz_tax", "payroll_tax_id", "Employer or payroll tax ID", Text, "")),
	sens(f("biz_tax", "sales_tax_permit", "Sales tax permit or resale certificate number", Text, "")),
	sens(f("biz_tax", "business_licence_number", "Business or trade licence number", Text, "")),
	f("biz_tax", "business_licence_expiry", "Business licence expiry date", Date, ""),
	// Business contact
	f("biz_contact", "company_email", "Company email", Email, "email"),
	f("biz_contact", "company_phone", "Company phone", Phone, "tel"),
	f("biz_contact", "invoice_email", "Invoicing or accounts email", Email, ""),
	f("biz_contact", "contact_name", "Contact person", Text, "name"),
	f("biz_contact", "contact_role", "Contact person's role", Text, "organization-title"),
	f("biz_contact", "contact_email", "Contact person's email", Email, ""),
	f("biz_contact", "contact_phone", "Contact person's phone", Phone, ""),
	// Business addresses
	f("biz_address", "company_address", "Business address", Textarea, ""),
	f("biz_address", "registered_address", "Registered office address", Textarea, ""),
	f("biz_address", "billing_address", "Billing address", Textarea, ""),
	f("biz_address", "shipping_address", "Shipping address", Textarea, ""),
	f("biz_address", "company_country", "Country of headquarters", Country, "country-name"),
	// Owners, directors & signatories
	sel("biz_owners", "ownership_structure", "Ownership", ownership),
	f("biz_owners", "owner_name", "Owner (main beneficial owner), full name", Text, ""),
	f("biz_owners", "owner_ownership_percent", "Owner's share (%)", Text, ""),
	f("biz_owners", "owner_role", "Owner's role in the business", Text, ""),
	sens(f("biz_owners", "owner_date_of_birth", "Owner's date of birth", Date, "")),
	f("biz_owners", "owner_nationality", "Owner's nationality", Country, ""),
	f("biz_owners", "owner_country_of_residence", "Owner's country of residence", Country, ""),
	f("biz_owners", "owner_address", "Owner's home address", Textarea, ""),
	sens(f("biz_owners", "owner_id_number", "Owner's passport or ID number", Text, "")),
	sens(f("biz_owners", "owner_tax_id", "Owner's tax ID", Text, "")),
	f("biz_owners", "owner_email", "Owner's email", Email, ""),
	f("biz_owners", "owner_phone", "Owner's phone", Phone, ""),
	f("biz_owners", "other_owners", "Other owners with 25% or more (name, share, date of birth)", Textarea, ""),
	f("biz_owners", "directors", "Directors or board members", Textarea, ""),
	f("biz_owners", "legal_representative", "Legal representative", Text, ""),
	f("biz_owners", "legal_representative_title", "Legal representative's title", Text, ""),
	f("biz_owners", "authorised_signatories", "Authorised signatories", Textarea, ""),
	f("biz_owners", "parent_company", "Parent company", Text, ""),
	f("biz_owners", "parent_company_country", "Parent company's country", Country, ""),
	f("biz_owners", "stock_exchange", "Stock exchange and ticker (if listed)", Text, ""),
	sel("biz_owners", "owner_politically_exposed", "Any owner is politically exposed", yesNo),
	sel("biz_owners", "owner_us_person", "Any owner is a US person", yesNo),
	// Finance & activity
	sel("biz_finance", "employee_count", "Employees", headcount),
	sel("biz_finance", "annual_revenue", "Annual revenue", revenue),
	f("biz_finance", "share_capital", "Share capital", Text, ""),
	f("biz_finance", "company_source_of_funds", "Source of funds", Text, ""),
	f("biz_finance", "expected_monthly_volume", "Expected monthly transaction volume", Text, ""),
	f("biz_finance", "countries_of_operation", "Countries of operation", Text, ""),
	// Regulation & compliance
	sel("biz_compliance", "regulated_entity", "Regulated by a financial or industry authority", yesNo),
	f("biz_compliance", "regulator_name", "Regulator", Text, ""),
	sens(f("biz_compliance", "regulatory_licence_number", "Regulatory licence number", Text, "")),
	sens(f("biz_compliance", "transparency_register_id", "Transparency register (beneficial owner) number", Text, "")),
	sens(f("biz_compliance", "giin", "FATCA GIIN", Text, "")),
	sel("biz_compliance", "crs_entity_type", "CRS entity type", crsEntities),
	sel("biz_compliance", "us_entity", "US entity for tax purposes", yesNo),
	f("biz_compliance", "compliance_officer", "Compliance or AML officer", Text, ""),
	// Business documents
	f("biz_docs", "incorporation_certificate_photo", "Certificate of incorporation", Photo, ""),
	f("biz_docs", "articles_of_association_photo", "Articles of association", Photo, ""),
	f("biz_docs", "vat_certificate_photo", "VAT registration certificate", Photo, ""),
	f("biz_docs", "business_proof_of_address_photo", "Proof of business address", Photo, ""),
	f("biz_docs", "owner_id_photo", "Owner's passport or ID card", Photo, ""),
	f("biz_docs", "bank_letter_photo", "Bank letter or statement", Photo, ""),
	f("biz_docs", "company_logo_photo", "Company logo", Photo, ""),
	f("biz_docs", "company_stamp_photo", "Company stamp or signature", Photo, ""),
	// Online
	f("online", "website", "Website", Text, "url"),
	f("online", "linkedin", "LinkedIn", Text, ""),
	f("online", "github", "GitHub", Text, ""),
	f("online", "twitter", "X / Twitter", Text, ""),
	f("online", "username", "Preferred username", Text, "username"),
	// Bank account
	f("financial", "bank_name", "Bank name", Text, ""),
	f("financial", "account_holder", "Account holder", Text, ""),
	sens(f("financial", "iban", "IBAN", Text, "")),
	sens(f("financial", "bic", "BIC / SWIFT", Text, "")),
	sens(f("financial", "bank_account_number", "Account number", Text, "")),
	sens(f("financial", "bank_routing_number", "Routing / sort code", Text, "")),
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
