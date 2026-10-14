package connections

// registryCore holds the social and service platforms of Registry.
var registryCore = map[string]PlatformDef{

	// ─── Social ────────────────────────────────────────────────────────────────

	"instagram": {
		ID:         "instagram",
		Name:       "Instagram Crawl",
		Category:   "social",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "📸",
	},
	"linkedin": {
		ID:         "linkedin",
		Name:       "LinkedIn Crawl",
		Category:   "social",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "💼",
	},
	"x": {
		ID:         "x",
		Name:       "X (Twitter) Crawl",
		Category:   "social",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "🐦",
	},
	"tiktok": {
		ID:         "tiktok",
		Name:       "TikTok Crawl",
		Category:   "social",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "🎵",
	},
	"reddit": {
		ID:         "reddit",
		Name:       "Reddit API",
		Category:   "social",
		ConnectVia: "API",
		// Register Reddit's OAuth app as "installed app" type (no client
		// secret) — this fits the existing generic PKCE flow in oauth.go
		// with no code changes; Reddit's "web app" type requires HTTP Basic
		// auth on token exchange, which the generic flow doesn't do.
		Methods: []AuthMethod{MethodOAuth},
		Fields:  map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://www.reddit.com/api/v1/authorize",
			TokenURL:     "https://www.reddit.com/api/v1/access_token",
			Scopes:       []string{"submit", "read", "identity"},
			CallbackPort: 9876,
			ExtraParams:  map[string]string{"duration": "permanent"},
		},
		IconEmoji: "👽",
	},
	"mastodon": {
		ID:         "mastodon",
		Name:       "Mastodon API",
		Category:   "social",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "instance_url", Label: "Instance URL (e.g. https://fosstodon.org)", Secret: false, Required: true},
				{Key: "access_token", Label: "Access Token", Secret: true, Required: true, HelpURL: "https://docs.joinmastodon.org/client/token/"},
			},
		},
		IconEmoji: "🐘",
	},
	"bluesky": {
		ID:         "bluesky",
		Name:       "Bluesky API",
		Category:   "social",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "identifier", Label: "Handle or Email", Secret: false, Required: true},
				{Key: "app_password", Label: "App Password", Secret: true, Required: true, HelpURL: "https://bsky.app/settings/app-passwords"},
			},
		},
		IconEmoji: "🦋",
	},
	"hackernews": {
		ID:         "hackernews",
		Name:       "Hacker News Crawl",
		Category:   "social",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "🔶",
	},
	"producthunt": {
		ID:         "producthunt",
		Name:       "Product Hunt",
		Category:   "social",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey, MethodBrowser},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "access_token", Label: "Developer Token", Secret: true, Required: true, HelpURL: "https://www.producthunt.com/v2/oauth/applications"},
			},
		},
		IconEmoji: "🐱",
	},
	"devto": {
		ID:         "devto",
		Name:       "Dev.to API",
		Category:   "social",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "api_key", Label: "API Key", Secret: true, Required: true, HelpURL: "https://dev.to/settings/extensions"},
			},
		},
		IconEmoji: "📝",
	},
	"hashnode": {
		ID:         "hashnode",
		Name:       "Hashnode API",
		Category:   "social",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "token", Label: "Personal Access Token", Secret: true, Required: true, HelpURL: "https://hashnode.com/settings/developer"},
			},
		},
		IconEmoji: "📓",
	},
	"gemini": {
		ID:         "gemini",
		Name:       "Gemini Crawl",
		Category:   "service",
		ConnectVia: "UI",
		Methods:    []AuthMethod{MethodBrowser},
		Fields:     map[AuthMethod][]CredentialField{},
		IconEmoji:  "✨",
	},
	// ─── Services ──────────────────────────────────────────────────────────────

	"github": {
		ID:         "github",
		Name:       "GitHub API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "token",
					Label:    "Personal Access Token",
					Secret:   true,
					Required: true,
					HelpURL:  "https://github.com/settings/tokens/new",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			Scopes:       []string{"repo", "read:user", "user:email"},
			CallbackPort: 9876,
		},
		IconEmoji: "🐙",
	},
	"notion": {
		ID:         "notion",
		Name:       "Notion API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "token",
					Label:    "Integration Token",
					Secret:   true,
					Required: true,
					HelpURL:  "https://www.notion.so/my-integrations",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://api.notion.com/v1/oauth/authorize",
			TokenURL:     "https://api.notion.com/v1/oauth/token",
			CallbackPort: 9876,
		},
		IconEmoji: "📝",
	},
	"airtable": {
		ID:         "airtable",
		Name:       "Airtable API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "api_key",
					Label:    "API Key",
					Secret:   true,
					Required: true,
					HelpURL:  "https://airtable.com/create/tokens",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://airtable.com/oauth2/v1/authorize",
			TokenURL:     "https://airtable.com/oauth2/v1/token",
			Scopes:       []string{"data.records:read", "data.records:write", "schema.bases:read"},
			CallbackPort: 9876,
		},
		IconEmoji: "📊",
	},
	"jira": {
		ID:         "jira",
		Name:       "Jira API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "email", Label: "Email", Secret: false, Required: true},
				{Key: "api_token", Label: "API Token", Secret: true, Required: true},
				{Key: "domain", Label: "Jira Domain", Secret: false, Required: true},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://auth.atlassian.com/authorize",
			TokenURL:     "https://auth.atlassian.com/oauth/token",
			CallbackPort: 9876,
		},
		IconEmoji: "🎯",
	},
	"linear": {
		ID:         "linear",
		Name:       "Linear API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "api_key",
					Label:    "API Key",
					Secret:   true,
					Required: true,
					HelpURL:  "https://linear.app/settings/api",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://linear.app/oauth/authorize",
			TokenURL:     "https://api.linear.app/oauth/token",
			Scopes:       []string{"read", "write"},
			CallbackPort: 9876,
		},
		IconEmoji: "📐",
	},
	"asana": {
		ID:         "asana",
		Name:       "Asana API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "access_token",
					Label:    "Personal Access Token",
					Secret:   true,
					Required: true,
					HelpURL:  "https://app.asana.com/0/my-apps",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://app.asana.com/-/oauth_authorize",
			TokenURL:     "https://app.asana.com/-/oauth_token",
			Scopes:       []string{"default"},
			CallbackPort: 9876,
		},
		IconEmoji: "✅",
	},
	"stripe": {
		ID:         "stripe",
		Name:       "Stripe API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "secret_key",
					Label:    "Secret Key",
					Secret:   true,
					Required: true,
					HelpURL:  "https://dashboard.stripe.com/apikeys",
				},
			},
		},
		IconEmoji: "💳",
	},
	"shopify": {
		ID:         "shopify",
		Name:       "Shopify API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{Key: "shop_domain", Label: "Shop Domain", Secret: false, Required: true},
				{Key: "access_token", Label: "Access Token", Secret: true, Required: true},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://{shop}/admin/oauth/authorize",
			TokenURL:     "https://{shop}/admin/oauth/access_token",
			CallbackPort: 9876,
		},
		IconEmoji: "🛍️",
	},
	"salesforce": {
		ID:         "salesforce",
		Name:       "Salesforce API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth},
		Fields:     map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://login.salesforce.com/services/oauth2/authorize",
			TokenURL:     "https://login.salesforce.com/services/oauth2/token",
			Scopes:       []string{"api", "refresh_token"},
			CallbackPort: 9876,
			ConnectFields: []CredentialField{{Key: "environment", Label: "Environment",
				HelpText: "production (default) or sandbox — a sandbox signs in at test.salesforce.com"}},
			Tailor: salesforceEnvironment,
		},
		IconEmoji: "☁️",
	},
	"hubspot": {
		ID:         "hubspot",
		Name:       "HubSpot API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth, MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "access_token",
					Label:    "Private App Access Token",
					Secret:   true,
					Required: true,
					HelpURL:  "https://app.hubspot.com/private-apps",
				},
			},
		},
		OAuth: &OAuthConfig{
			AuthURL:      "https://app.hubspot.com/oauth/authorize",
			TokenURL:     "https://api.hubapi.com/oauth/v1/token",
			CallbackPort: 9876,
		},
		IconEmoji: "🧡",
	},
	"google_sheets": {
		ID:         "google_sheets",
		Name:       "Google Sheets API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth},
		Fields:     map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			Scopes:       []string{"https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/drive.readonly"},
			CallbackPort: 9876,
			ExtraParams:  map[string]string{"access_type": "offline", "prompt": "consent"},
		},
		IconEmoji: "📗",
	},
	"gmail": {
		ID:         "gmail",
		Name:       "Gmail API",
		Category:   "service",
		ConnectVia: "API",
		// App Password is intentionally not offered: internal/nodes/service/gmail.go
		// calls the Gmail REST API (gmail.googleapis.com), which is OAuth-only — an
		// app password can't authenticate against it (that only works for raw
		// SMTP/IMAP, which this node doesn't use).
		Methods: []AuthMethod{MethodOAuth},
		Fields:  map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			Scopes:       []string{"https://www.googleapis.com/auth/gmail.modify"},
			CallbackPort: 9876,
			ExtraParams:  map[string]string{"access_type": "offline", "prompt": "consent"},
		},
		IconEmoji: "📧",
	},
	"google_drive": {
		ID:         "google_drive",
		Name:       "Google Drive API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth},
		Fields:     map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			Scopes:       []string{"https://www.googleapis.com/auth/drive"},
			CallbackPort: 9876,
			ExtraParams:  map[string]string{"access_type": "offline", "prompt": "consent"},
		},
		IconEmoji: "📁",
	},
	"youtube": {
		ID:         "youtube",
		Name:       "YouTube API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodOAuth},
		Fields:     map[AuthMethod][]CredentialField{},
		OAuth: &OAuthConfig{
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			Scopes:       []string{"https://www.googleapis.com/auth/youtube.upload", "https://www.googleapis.com/auth/youtube.force-ssl"},
			CallbackPort: 9876,
			ExtraParams:  map[string]string{"access_type": "offline", "prompt": "consent"},
		},
		IconEmoji: "📺",
	},
	"huggingface": {
		ID:         "huggingface",
		Name:       "Hugging Face API",
		Category:   "service",
		ConnectVia: "API",
		Methods:    []AuthMethod{MethodAPIKey},
		Fields: map[AuthMethod][]CredentialField{
			MethodAPIKey: {
				{
					Key:      "api_key",
					Label:    "API Key",
					Secret:   true,
					Required: true,
					HelpText: "Your Hugging Face access token. Find it at huggingface.co/settings/tokens.",
				},
			},
		},
		IconEmoji: "🤗",
	},
}
