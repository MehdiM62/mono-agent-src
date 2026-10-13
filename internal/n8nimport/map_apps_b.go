package n8nimport

import (
	"fmt"
	"sort"
	"strings"
)

func init() {
	Register("n8n-nodes-base.hubspot", mapHubspot)
	Register("n8n-nodes-base.salesforce", mapSalesforce)
	Register("n8n-nodes-base.shopify", mapShopify)
	Register("n8n-nodes-base.stripe", mapStripe)
	Register("n8n-nodes-base.youTube", mapYouTube)
	Register("n8n-nodes-base.reddit", mapReddit)
}

// hubspotNames maps n8n's camelCase field names to HubSpot property names.
var hubspotNames = map[string]string{
	"firstName": "firstname", "lastName": "lastname", "companyName": "company", "phoneNumber": "phone",
	"jobTitle": "jobtitle", "websiteUrl": "website", "postalCode": "zip", "mobilePhoneNumber": "mobilephone",
	"dealName": "dealname", "closeDate": "closedate", "dealType": "dealtype", "dealOwner": "hubspot_owner_id",
	"stage": "dealstage", "lifeCycleStage": "lifecyclestage", "leadStatus": "hs_lead_status",
}

// hubspotProps builds a properties map from a collection of n8n fields.
func hubspotProps(c *MapContext, coll string, props map[string]interface{}) {
	m := c.paramMap(coll)
	for _, k := range sortedKeys(m) {
		p := coll + "." + k
		if k == "customPropertiesUi" {
			if l, ok := m[k].(map[string]interface{})["customPropertiesValues"].([]interface{}); ok {
				for _, raw := range l {
					if cp, ok := raw.(map[string]interface{}); ok {
						props[fmt.Sprint(cp["property"])] = c.Translate(p, cp["value"])
					}
				}
			}
			c.Ignore(p)
			continue
		}
		if _, isMap := m[k].(map[string]interface{}); isMap {
			continue // association/option sub-collections are reported as unused
		}
		name := hubspotNames[k]
		if name == "" {
			name = strings.ToLower(k)
		}
		props[name] = c.Value(p)
	}
}

func mapHubspot(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("hubspot", "hubspotApi", "hubspotAppToken", "hubspotOAuth2Api")}
	c.Ignore("authentication", "options.resolveData")
	defRes := "contact"
	if !c.version(2) {
		defRes = "deal"
	}
	res := c.Literal("resource", defRes)
	op := c.Literal("operation", map[string]string{"contact": "upsert"}[res])
	if op == "" {
		op = c.Literal("operation", "create")
	}
	props := map[string]interface{}{}
	id := func(param string) { cfg["object_id"] = c.Str(param, "") }
	list := func(mono string) {
		cfg["operation"] = mono
		cfg["limit"] = c.Float("limit", 100)
		c.Ignore("returnAll", "additionalFields", "filters")
	}
	switch res + "." + op {
	case "contact.upsert":
		cfg["operation"] = "create_contact"
		props["email"] = c.Str("email", "")
		hubspotProps(c, "additionalFields", props)
		c.Note("upsert became create_contact: an existing contact with this email makes the node fail")
	case "contact.get":
		cfg["operation"] = "get_contact"
		id("contactId")
		c.Ignore("additionalFields")
	case "contact.getAll":
		list("list_contacts")
	case "deal.create":
		cfg["operation"] = "create_deal"
		props["dealstage"] = c.Str("stage", "")
		hubspotProps(c, "additionalFields", props)
	case "deal.update":
		cfg["operation"] = "update_deal"
		id("dealId")
		hubspotProps(c, "updateFields", props)
	case "deal.getAll":
		list("list_deals")
	case "company.create":
		cfg["operation"] = "create_company"
		props["name"] = c.Str("name", "")
		hubspotProps(c, "additionalFields", props)
	case "company.update":
		cfg["operation"] = "update_company"
		id("companyId")
		hubspotProps(c, "updateFields", props)
	case "company.getAll":
		list("list_companies")
	default:
		return c.OperationUnsupported(res, op)
	}
	if len(props) > 0 {
		cfg["properties"] = props
	}
	return Mapping{Type: "service.hubspot", Config: cfg}
}

var salesforceObjects = map[string]string{"lead": "Lead", "contact": "Contact", "account": "Account",
	"opportunity": "Opportunity", "case": "Case", "task": "Task", "attachment": "Attachment", "user": "User"}

// salesforceNames are Salesforce API names for n8n's lower-case fields.
var salesforceNames = map[string]string{"lastname": "LastName", "firstname": "FirstName", "company": "Company",
	"email": "Email", "phone": "Phone", "status": "Status", "title": "Title", "description": "Description",
	"website": "Website", "industry": "Industry", "city": "City", "country": "Country", "street": "Street",
	"postalCode": "PostalCode", "state": "State", "mobilePhone": "MobilePhone", "leadSource": "LeadSource",
	"name": "Name", "closeDate": "CloseDate", "stageName": "StageName", "amount": "Amount", "type": "Type",
	"subject": "Subject", "priority": "Priority", "owner": "OwnerId", "accountId": "AccountId"}

func salesforceFields(c *MapContext, coll string, fields map[string]interface{}) {
	m := c.paramMap(coll)
	for _, k := range sortedKeys(m) {
		p := coll + "." + k
		if k == "customFieldsUi" {
			if l, ok := m[k].(map[string]interface{})["customFieldsValues"].([]interface{}); ok {
				for _, raw := range l {
					if cf, ok := raw.(map[string]interface{}); ok {
						fields[fmt.Sprint(cf["fieldId"])] = c.Translate(p, cf["value"])
					}
				}
			}
			c.Ignore(p)
			continue
		}
		name := salesforceNames[k]
		if name == "" {
			name = strings.ToUpper(k[:1]) + k[1:]
		}
		fields[name] = c.Value(p)
	}
}

func mapSalesforce(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("salesforce", "salesforceOAuth2Api", "salesforceJwtApi")}
	c.Ignore("authentication")
	res, op := resOp(c, "lead", "create", map[string]string{"search": "query", "flow": "invoke", "user": "get", "document": "upload"})
	if res == "search" {
		cfg["operation"] = "query"
		cfg["soql"] = c.Str("query", "")
		return Mapping{Type: "service.salesforce", Config: cfg}
	}
	object := salesforceObjects[res]
	if res == "customObject" {
		object = c.Str("customObject", "")
	}
	if object == "" {
		return c.OperationUnsupported(res, op)
	}
	idParam := res + "Id"
	if res == "customObject" {
		idParam = "recordId"
	}
	cfg["object_type"] = object
	fields := map[string]interface{}{}
	switch op {
	case "create", "upsert":
		cfg["operation"] = "create_record"
		for _, k := range []string{"company", "lastname", "name", "closeDate", "stageName", "subject", "status"} {
			if c.Has(k) {
				fields[salesforceNames[k]] = c.Value(k)
			}
		}
		salesforceFields(c, "additionalFields", fields)
		if len(c.paramList("customFieldsUi.customFieldsValues")) > 0 {
			for i, cf := range c.paramList("customFieldsUi.customFieldsValues") {
				fields[fmt.Sprint(cf["fieldId"])] = c.Translate(fmt.Sprintf("customFieldsUi[%d]", i), cf["value"])
			}
		}
		if op == "upsert" {
			c.Warn("operation", "upsert", "creates a new record; it does not match on the external id")
			c.Ignore("externalId", "externalIdValue")
		}
	case "update":
		cfg["operation"] = "update_record"
		cfg["record_id"] = c.Str(idParam, "")
		salesforceFields(c, "updateFields", fields)
		for i, cf := range c.paramList("customFieldsUi.customFieldsValues") {
			fields[fmt.Sprint(cf["fieldId"])] = c.Translate(fmt.Sprintf("customFieldsUi[%d]", i), cf["value"])
		}
	case "get":
		cfg["operation"] = "get_record"
		cfg["record_id"] = c.Str(idParam, "")
	case "delete":
		cfg["operation"] = "delete_record"
		cfg["record_id"] = c.Str(idParam, "")
	case "getAll":
		cfg["operation"] = "query"
		cfg["soql"] = salesforceQuery(c, object)
	default:
		return c.OperationUnsupported(res, op)
	}
	if len(fields) > 0 {
		cfg["fields"] = fields
	}
	return Mapping{Type: "service.salesforce", Config: cfg}
}

// salesforceQuery builds the SOQL for getAll from fields, conditions and limit.
func salesforceQuery(c *MapContext, object string) string {
	fields := "Id"
	switch f := c.Raw("options.fields").(type) {
	case string:
		if f != "" {
			fields = f
		}
	case []interface{}:
		var parts []string
		for _, x := range f {
			parts = append(parts, fmt.Sprint(x))
		}
		sort.Strings(parts)
		if len(parts) > 0 {
			fields = strings.Join(parts, ",")
		}
	}
	q := "SELECT " + fields + " FROM " + object
	var where []string
	for i, cond := range c.paramList("options.conditionsUi.conditionValues") {
		opName := fmt.Sprint(cond["operation"])
		if opName == "equal" {
			opName = "="
		}
		v := c.Translate(fmt.Sprintf("options.conditionsUi[%d].value", i), cond["value"])
		where = append(where, fmt.Sprintf("%v %s '%v'", cond["field"], opName, v))
	}
	c.Ignore("options.conditionsUi")
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if !c.Bool("returnAll", false) {
		q += fmt.Sprintf(" LIMIT %d", int(c.Float("limit", 50)))
	}
	return q
}

func mapShopify(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("shopify", "shopifyApi", "shopifyAccessTokenApi", "shopifyOAuth2Api")}
	c.Ignore("authentication")
	res, op := resOp(c, "order", "create")
	switch res + "." + op {
	case "order.getAll":
		cfg["operation"] = "list_orders"
		cfg["limit"] = c.Float("limit", 50)
		cfg["status"] = c.Literal("options.status", "open")
		c.Ignore("returnAll")
	case "order.get":
		cfg["operation"] = "get_order"
		cfg["order_id"] = c.Str("orderId", "")
		c.Ignore("options.fields")
	case "order.update":
		cfg["operation"] = "update_order"
		cfg["order_id"] = c.Str("orderId", "")
	case "product.create":
		cfg["operation"] = "create_product"
		cfg["title"] = c.Str("title", "")
		copyFields(c, cfg, "additionalFields", map[string]string{"vendor": "vendor", "product_type": "product_type"})
	case "product.update":
		cfg["operation"] = "update_product"
		cfg["product_id"] = c.Str("productId", "")
		copyFields(c, cfg, "updateFields", map[string]string{"title": "title", "vendor": "vendor", "product_type": "product_type"})
	case "product.get":
		cfg["operation"] = "get_product"
		cfg["product_id"] = c.Str("productId", "")
		c.Ignore("additionalFields.fields")
	case "product.getAll":
		cfg["operation"] = "list_products"
		cfg["limit"] = c.Float("limit", 50)
		c.Ignore("returnAll")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.shopify", Config: cfg}
}

func mapStripe(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("stripe", "stripeApi")}
	res, op := resOp(c, "balance", "get", map[string]string{"charge": "get", "coupon": "create", "customer": "get", "customerCard": "get", "source": "get", "token": "create", "meterEvent": "create"})
	switch res + "." + op {
	case "customer.create":
		cfg["operation"] = "create_customer"
		cfg["name"] = c.Str("name", "")
		copyFields(c, cfg, "additionalFields", map[string]string{"email": "email"})
	case "customer.get":
		cfg["operation"] = "get_customer"
		cfg["customer_id"] = c.Str("customerId", "")
	case "customer.getAll":
		cfg["operation"] = "list_customers"
		cfg["limit"] = c.Float("limit", 50)
		c.Ignore("returnAll")
	case "charge.create":
		cfg["operation"] = "create_charge"
		cfg["customer_id"] = c.Str("customerId", "")
		cfg["amount"] = c.Num("amount", 0)
		cfg["currency"] = c.Str("currency", "usd")
		cfg["source"] = c.Str("source", "")
	case "charge.getAll":
		cfg["operation"] = "list_charges"
		cfg["limit"] = c.Float("limit", 50)
		c.Ignore("returnAll")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.stripe", Config: cfg}
}

func mapYouTube(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("youtube", "youTubeOAuth2Api")}
	res, op := resOp(c, "channel", "getAll", map[string]string{"playlist": "getAll", "playlistItem": "add", "video": "getAll", "videoCategory": "getAll"})
	switch res + "." + op {
	case "video.upload":
		cfg["operation"] = "upload_video"
		cfg["title"] = c.Str("title", "")
		cfg["category_id"] = c.Str("categoryId", "22")
		cfg["video_file_path"] = ""
		copyFields(c, cfg, "options", map[string]string{"description": "description", "tags": "tags", "privacyStatus": "privacy_status"})
		c.Ignore("regionCode", "binaryProperty")
		c.Warn("binaryProperty", nil, "upload_video sends a file from disk: set video_file_path (n8n uploaded binary data)")
	case "video.get":
		cfg["operation"] = "get_video_stats"
		cfg["video_id"] = c.Str("videoId", "")
		c.Ignore("part", "options")
	case "video.getAll":
		cfg["operation"] = "search_videos"
		cfg["query"] = c.Str("filters.q", "")
		cfg["max_results"] = c.Float("limit", 25)
		c.Ignore("returnAll", "part", "options")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: "service.youtube", Config: cfg}
}

func mapReddit(c *MapContext) Mapping {
	cfg := map[string]interface{}{"credential_id": c.Credential("reddit", "redditOAuth2Api")}
	res, op := resOp(c, "post", "create", map[string]string{"postComment": "create", "profile": "get", "subreddit": "get", "user": "get"})
	typ := "service.reddit"
	switch res + "." + op {
	case "post.create":
		cfg["operation"] = "submit_post"
		cfg["subreddit"] = c.Str("subreddit", "")
		cfg["title"] = c.Str("title", "")
		kind := c.Literal("kind", "self")
		if kind == "self" {
			cfg["kind"], cfg["text"] = "self", c.Str("text", "")
		} else {
			cfg["kind"], cfg["url"] = "link", c.Str("url", "")
		}
		c.Ignore("resubmit")
	case "post.getAll":
		if cat := c.Literal("filters.category", "hot"); cat != "hot" {
			c.Warn("filters.category", cat, "mono-agent reads the hot posts")
		}
		cfg["operation"] = "get_hot"
		cfg["subreddit"] = c.Str("subreddit", "")
		cfg["limit"] = c.Float("limit", 100)
		c.Ignore("returnAll")
	case "postComment.create":
		cfg["operation"] = "comment"
		cfg["thing_id"] = "t3_" + c.Str("postId", "")
		cfg["text"] = c.Str("commentText", "")
	case "postComment.reply":
		cfg["operation"] = "comment"
		cfg["thing_id"] = "t1_" + c.Str("commentId", "")
		cfg["text"] = c.Str("replyText", "")
	case "postComment.getAll":
		typ = "comm.reddit"
		cfg["operation"] = "list_comments"
		cfg["thing_id"] = c.Str("postId", "")
		c.Ignore("subreddit", "returnAll", "limit")
	default:
		return c.OperationUnsupported(res, op)
	}
	return Mapping{Type: typ, Config: cfg}
}
