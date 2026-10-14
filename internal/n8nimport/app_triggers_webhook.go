package n8nimport

// webhookTriggers lists n8n's webhook-based app triggers by node name
// (without the "n8n-nodes-base." prefix). Every one becomes a
// trigger.webhook; see map_app_trigger.go. The vendor URL checks the
// webhook server answers itself (HEAD, hub.challenge, validationToken,
// X-Hook-Secret, Slack url_verification, SNS confirmation) need no entry;
// Handshake names the ones it does not.
var webhookTriggers = map[string]hookSpec{
	"activeCampaignTrigger":        {Vendor: "ActiveCampaign", Where: "ActiveCampaign (Settings → Developer → Webhooks)"},
	"acuitySchedulingTrigger":      {Vendor: "Acuity Scheduling", Where: "Acuity Scheduling (Integrations → API → Webhooks)"},
	"affinityTrigger":              {Vendor: "Affinity", Where: "Affinity (webhook subscriptions API)"},
	"asanaTrigger":                 {Vendor: "Asana", Where: "Asana (POST /webhooks for the resource)"},
	"autopilotTrigger":             {Vendor: "Autopilot", Where: "Autopilot (Settings → App Connections → Webhooks)"},
	"awsSnsTrigger":                {Vendor: "AWS SNS", Where: "AWS SNS (an HTTPS subscription on the topic)"},
	"bitbucketTrigger":             {Vendor: "Bitbucket", Where: "Bitbucket (Repository or Workspace settings → Webhooks)", DedupeHeader: "X-Request-UUID"},
	"boxTrigger":                   {Vendor: "Box", Where: "Box (Developer Console → Webhooks)"},
	"sendInBlueTrigger":            {Vendor: "Brevo", Where: "Brevo (Settings → Webhooks)"},
	"calTrigger":                   {Vendor: "Cal.com", Where: "Cal.com (Settings → Developer → Webhooks)"},
	"calendlyTrigger":              {Vendor: "Calendly", Where: "Calendly (webhook subscriptions API)", Register: registerCalendly},
	"chargebeeTrigger":             {Vendor: "Chargebee", Where: "Chargebee (Settings → Configure Chargebee → Webhooks)"},
	"ciscoWebexTrigger":            {Vendor: "Webex", Where: "Webex (POST /webhooks)"},
	"clickUpTrigger":               {Vendor: "ClickUp", Where: "ClickUp (POST /team/{team_id}/webhook)"},
	"convertKitTrigger":            {Vendor: "Kit (ConvertKit)", Where: "Kit (automation webhooks API)"},
	"copperTrigger":                {Vendor: "Copper", Where: "Copper (POST /webhooks)"},
	"currentsTrigger":              {Vendor: "Currents", Where: "Currents (Project settings → Integrations → Webhooks)"},
	"customerIoTrigger":            {Vendor: "Customer.io", Where: "Customer.io (Data & Integrations → Reporting webhooks)"},
	"emeliaTrigger":                {Vendor: "Emelia", Where: "Emelia (campaign webhooks)"},
	"eventbriteTrigger":            {Vendor: "Eventbrite", Where: "Eventbrite (Account settings → Developer links → Webhooks)"},
	"facebookTrigger":              {Vendor: "Facebook", Where: "the Meta app dashboard (Webhooks product)"},
	"facebookLeadAdsTrigger":       {Vendor: "Facebook Lead Ads", Where: "the Meta app dashboard (Webhooks → Page → leadgen), with the page subscribed to the app"},
	"figmaTrigger":                 {Vendor: "Figma", Where: "Figma (POST /v2/webhooks for the team)"},
	"flowTrigger":                  {Vendor: "Flow", Where: "Flow (integration webhooks)"},
	"formIoTrigger":                {Vendor: "Form.io", Where: "Form.io (the form's Actions → Webhook)", Shape: shapeFormIo},
	"formstackTrigger":             {Vendor: "Formstack", Where: "Formstack (the form's Settings → Emails & Actions → Webhook)", Shape: shapeFormstack},
	"getResponseTrigger":           {Vendor: "GetResponse", Where: "GetResponse (account callbacks)", Method: "GET"},
	"githubTrigger":                {Vendor: "GitHub", Where: "GitHub (repository Settings → Webhooks, content type application/json)", DedupeHeader: "X-GitHub-Delivery"},
	"gitlabTrigger":                {Vendor: "GitLab", Where: "GitLab (project Settings → Webhooks)"},
	"gumroadTrigger":               {Vendor: "Gumroad", Where: "Gumroad (resource subscriptions API)"},
	"helpScoutTrigger":             {Vendor: "Help Scout", Where: "Help Scout (Manage → Apps → Webhooks)"},
	"hubspotTrigger":               {Vendor: "HubSpot", Where: "the HubSpot developer app (Webhooks subscriptions)"},
	"invoiceNinjaTrigger":          {Vendor: "Invoice Ninja", Where: "Invoice Ninja (Settings → Workflow → Webhooks)"},
	"jiraTrigger":                  {Vendor: "Jira", Where: "Jira (System → WebHooks)"},
	"jotFormTrigger":               {Vendor: "Jotform", Where: "Jotform (the form's Settings → Integrations → WebHooks)", Shape: shapeJotform},
	"keapTrigger":                  {Vendor: "Keap", Where: "Keap (REST hooks API)"},
	"koBoToolboxTrigger":           {Vendor: "KoBoToolbox", Where: "KoBoToolbox (the project's Settings → REST Services)"},
	"lemlistTrigger":               {Vendor: "lemlist", Where: "lemlist (Settings → Integrations → Webhooks)"},
	"linearTrigger":                {Vendor: "Linear", Where: "Linear (Settings → API → Webhooks)", DedupeHeader: "Linear-Delivery"},
	"loneScaleTrigger":             {Vendor: "LoneScale", Where: "LoneScale (workflow webhooks)"},
	"mailchimpTrigger":             {Vendor: "Mailchimp", Where: "Mailchimp (Audience → Settings → Webhooks)", Handshake: "a GET to the URL that must answer 200; trigger.webhook answers a GET on a POST route with 405"},
	"mailerLiteTrigger":            {Vendor: "MailerLite", Where: "MailerLite (Integrations → Webhooks)"},
	"mailjetTrigger":               {Vendor: "Mailjet", Where: "Mailjet (Account settings → Event tracking)"},
	"mauticTrigger":                {Vendor: "Mautic", Where: "Mautic (Settings → Webhooks)"},
	"microsoftTeamsTrigger":        {Vendor: "Microsoft Teams", Where: "Microsoft Graph (POST /subscriptions for the team, channel or chat)"},
	"netlifyTrigger":               {Vendor: "Netlify", Where: "Netlify (Site configuration → Notifications → Outgoing webhooks)"},
	"onfleetTrigger":               {Vendor: "Onfleet", Where: "Onfleet (Settings → API & Webhooks)", Handshake: "a GET with a check parameter that must be echoed back as text; trigger.webhook does not answer that check"},
	"payPalTrigger":                {Vendor: "PayPal", Where: "the PayPal developer app (Webhooks)"},
	"pipedriveTrigger":             {Vendor: "Pipedrive", Where: "Pipedrive (Tools and apps → Webhooks)"},
	"postmarkTrigger":              {Vendor: "Postmark", Where: "Postmark (the server's message stream → Webhooks)"},
	"pushcutTrigger":               {Vendor: "Pushcut", Where: "Pushcut (an online action pointing at the URL)"},
	"shopifyTrigger":               {Vendor: "Shopify", Where: "Shopify (Settings → Notifications → Webhooks, JSON format)", DedupeHeader: "X-Shopify-Webhook-Id"},
	"slackTrigger":                 {Vendor: "Slack", Where: "the Slack app's Event Subscriptions (Request URL)"},
	"stravaTrigger":                {Vendor: "Strava", Where: "Strava (POST /push_subscriptions)", Handshake: "a GET with hub.challenge that must be echoed back as JSON {\"hub.challenge\": …}; trigger.webhook echoes it as plain text, which Strava rejects"},
	"stripeTrigger":                {Vendor: "Stripe", Where: "Stripe (Developers → Webhooks)"},
	"surveyMonkeyTrigger":          {Vendor: "SurveyMonkey", Where: "SurveyMonkey (POST /webhooks)"},
	"taigaTrigger":                 {Vendor: "Taiga", Where: "Taiga (project Admin → Integrations → Webhooks)"},
	"telegramTrigger":              {Vendor: "Telegram", Where: "Telegram (setWebhook for the bot)"},
	"theHiveTrigger":               {Vendor: "TheHive", Where: "TheHive (application.conf webhooks)"},
	"theHiveProjectTrigger":        {Vendor: "TheHive", Where: "TheHive (Platform management → Notifications → Webhook)"},
	"trelloTrigger":                {Vendor: "Trello", Where: "Trello (POST /1/webhooks for the model id)"},
	"twilioTrigger":                {Vendor: "Twilio", Where: "Twilio (the phone number's messaging/voice webhook, or Event Streams)"},
	"typeformTrigger":              {Vendor: "Typeform", Where: "Typeform (the form's Connect → Webhooks)", Shape: shapeTypeform, Register: registerTypeform},
	"venafiTlsProtectCloudTrigger": {Vendor: "Venafi TLS Protect Cloud", Where: "Venafi TLS Protect Cloud (connectors webhook)"},
	"webflowTrigger":               {Vendor: "Webflow", Where: "Webflow (Site settings → Apps & integrations → Webhooks)"},
	"whatsAppTrigger":              {Vendor: "WhatsApp", Where: "the Meta app dashboard (WhatsApp → Configuration → Webhook)"},
	"wiseTrigger":                  {Vendor: "Wise", Where: "Wise (Settings → Developer tools → Webhooks)"},
	"wooCommerceTrigger":           {Vendor: "WooCommerce", Where: "WooCommerce (Settings → Advanced → Webhooks)", DedupeHeader: "X-WC-Webhook-Delivery-ID"},
	"workableTrigger":              {Vendor: "Workable", Where: "Workable (POST /subscriptions)"},
	"wufooTrigger":                 {Vendor: "Wufoo", Where: "Wufoo (the form's Notifications → WebHook)", Shape: shapeWufoo},
	"zendeskTrigger":               {Vendor: "Zendesk", Where: "Zendesk (Admin Center → Apps and integrations → Webhooks, plus a trigger that notifies it)"},
}

// registerTypeform has activation register the form's webhook through
// trigger.webhook's typeform registrar, as n8n did.
func registerTypeform(c *MapContext, cfg map[string]interface{}) {
	formID := c.Literal("formId", "")
	if formID == "" {
		c.Warn("formId", nil, "no form id: set register_config.form_id before activating")
	}
	cfg["register_with"] = "typeform"
	cfg["register_config"] = map[string]interface{}{"form_id": formID}
	cfg["credential_id"] = c.Credential("typeform", "typeformApi", "typeformOAuth2Api")
}

// registerCalendly has activation create the webhook subscription through
// trigger.webhook's calendly registrar. n8n looked the organization and
// user up from the token at activation; the registrar takes them as
// settings.
func registerCalendly(c *MapContext, cfg map[string]interface{}) {
	scope := c.Literal("scope", "user")
	rc := map[string]interface{}{"scope": scope}
	var events []interface{}
	if list, ok := c.Raw("events").([]interface{}); ok {
		events = list
	}
	if len(events) == 0 {
		c.Warn("events", nil, "no events chosen: set register_config.events (invitee.created, invitee.canceled) before activating")
	}
	rc["events"] = events
	cfg["register_with"] = "calendly"
	cfg["register_config"] = rc
	cfg["credential_id"] = c.Credential("calendly", "calendlyApi", "calendlyOAuth2Api")
	need := "organization"
	if scope == "user" {
		need = "organization and user"
	}
	c.Warn("scope", scope, "set register_config's %s before activating: the current_organization and uri of GET https://api.calendly.com/users/me (n8n looked them up itself)", need)
}
