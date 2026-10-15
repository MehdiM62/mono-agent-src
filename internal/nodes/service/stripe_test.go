package service

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// stripeTestResponse satisfies both object and list operations.
const stripeTestResponse = `{"id":"obj_1","object":"thing","data":[{"id":"obj_1"}]}`

func newStripeCapture(t *testing.T) *apiCapture {
	t.Helper()
	c := newAPICapture(t, func(*http.Request) (int, string) { return http.StatusOK, stripeTestResponse })
	c.point(t, &stripeBaseURL, "/v1")
	return c
}

func TestStripeOperationsRequests(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]interface{}
		method     string
		path       string
		query      map[string]string // checked for GET/DELETE
		form       map[string]string // checked for POST
		formAbsent []string
	}{
		{name: "list_customers", config: map[string]interface{}{"operation": "list_customers"},
			method: "GET", path: "/v1/customers", query: map[string]string{"limit": "10"}},
		{name: "create_customer", config: map[string]interface{}{"operation": "create_customer", "email": "a@b.co", "name": "Ann"},
			method: "POST", path: "/v1/customers", form: map[string]string{"email": "a@b.co", "name": "Ann"}},
		{name: "get_customer", config: map[string]interface{}{"operation": "get_customer", "customer_id": "cus_1"},
			method: "GET", path: "/v1/customers/cus_1"},
		{name: "list_charges", config: map[string]interface{}{"operation": "list_charges", "customer_id": "cus_1", "limit": float64(5)},
			method: "GET", path: "/v1/charges", query: map[string]string{"limit": "5", "customer": "cus_1"}},
		{name: "create_charge (legacy)", config: map[string]interface{}{"operation": "create_charge", "amount": float64(500), "source": "tok_visa", "customer_id": "cus_1"},
			method: "POST", path: "/v1/charges", form: map[string]string{"amount": "500", "currency": "usd", "source": "tok_visa", "customer": "cus_1"}},
		{name: "list_subscriptions", config: map[string]interface{}{"operation": "list_subscriptions", "customer_id": "cus_1"},
			method: "GET", path: "/v1/subscriptions", query: map[string]string{"limit": "10", "customer": "cus_1"}},
		{name: "create_subscription", config: map[string]interface{}{"operation": "create_subscription", "customer_id": "cus_1", "price_id": "price_1"},
			method: "POST", path: "/v1/subscriptions", form: map[string]string{"customer": "cus_1", "items[0][price]": "price_1"}},
		{name: "cancel_subscription", config: map[string]interface{}{"operation": "cancel_subscription", "subscription_id": "sub_1"},
			method: "DELETE", path: "/v1/subscriptions/sub_1"},
		{name: "list_products", config: map[string]interface{}{"operation": "list_products", "limit": float64(3)},
			method: "GET", path: "/v1/products", query: map[string]string{"limit": "3"}},
		{name: "create_payment_intent", config: map[string]interface{}{"operation": "create_payment_intent", "amount": float64(1200), "currency": "eur", "customer_id": "cus_1"},
			method: "POST", path: "/v1/payment_intents", form: map[string]string{"amount": "1200", "currency": "eur", "customer": "cus_1"}},
		{name: "get_payment_intent", config: map[string]interface{}{"operation": "get_payment_intent", "payment_intent_id": "pi_1"},
			method: "GET", path: "/v1/payment_intents/pi_1"},
		{name: "list_payment_intents", config: map[string]interface{}{"operation": "list_payment_intents", "customer_id": "cus_1", "limit": float64(7)},
			method: "GET", path: "/v1/payment_intents", query: map[string]string{"limit": "7", "customer": "cus_1"}},
		{name: "create_refund (payment intent, partial)", config: map[string]interface{}{"operation": "create_refund", "payment_intent_id": "pi_1", "amount": float64(250), "reason": "requested_by_customer"},
			method: "POST", path: "/v1/refunds", form: map[string]string{"payment_intent": "pi_1", "amount": "250", "reason": "requested_by_customer"}, formAbsent: []string{"charge"}},
		{name: "create_refund (charge, full)", config: map[string]interface{}{"operation": "create_refund", "charge_id": "ch_1"},
			method: "POST", path: "/v1/refunds", form: map[string]string{"charge": "ch_1"}, formAbsent: []string{"amount", "reason", "payment_intent"}},
		{name: "create_checkout_session (list line items)", config: map[string]interface{}{
			"operation": "create_checkout_session", "success_url": "https://x.test/ok", "cancel_url": "https://x.test/no", "customer_id": "cus_1",
			"line_items": []interface{}{
				map[string]interface{}{"price": "price_a", "quantity": float64(2)},
				map[string]interface{}{"price": "price_b"},
			}},
			method: "POST", path: "/v1/checkout/sessions", form: map[string]string{
				"mode": "payment", "success_url": "https://x.test/ok", "cancel_url": "https://x.test/no", "customer": "cus_1",
				"line_items[0][price]": "price_a", "line_items[0][quantity]": "2",
				"line_items[1][price]": "price_b", "line_items[1][quantity]": "1",
			}, formAbsent: []string{"currency"}},
		{name: "create_checkout_session (JSON string, subscription)", config: map[string]interface{}{
			"operation": "create_checkout_session", "mode": "subscription", "success_url": "https://x.test/ok",
			"line_items": `[{"price": "price_m", "quantity": "3"}]`},
			method: "POST", path: "/v1/checkout/sessions", form: map[string]string{
				"mode": "subscription", "line_items[0][price]": "price_m", "line_items[0][quantity]": "3",
			}, formAbsent: []string{"cancel_url", "customer"}},
		{name: "create_checkout_session (setup)", config: map[string]interface{}{
			"operation": "create_checkout_session", "mode": "setup", "success_url": "https://x.test/ok", "currency": "gbp"},
			method: "POST", path: "/v1/checkout/sessions", form: map[string]string{"mode": "setup", "currency": "gbp"},
			formAbsent: []string{"line_items[0][price]"}},
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_test_123:"))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newStripeCapture(t)
			cfg := capMerge(map[string]interface{}{"secret_key": "sk_test_123"}, tc.config)
			items := capRun(t, &StripeNode{}, workflow.NodeInput{}, cfg)
			if len(items) != 1 || items[0].JSON["id"] != "obj_1" {
				t.Errorf("items = %+v, want one obj_1 item", items)
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != wantAuth {
				t.Errorf("Authorization = %q, want %q", got, wantAuth)
			}
			capWantValues(t, "query", req.Query, tc.query)
			if tc.method == "POST" {
				if ct := req.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
					t.Errorf("Content-Type = %q, want form encoding", ct)
				}
				form := req.form(t)
				capWantValues(t, "form", form, tc.form)
				for _, k := range tc.formAbsent {
					if form.Has(k) {
						t.Errorf("form has %s=%q, want it absent", k, form.Get(k))
					}
				}
			} else if len(req.Body) != 0 {
				t.Errorf("%s body = %q, want empty", req.Method, req.Body)
			}
		})
	}
}

func TestStripeLegacyAPIKeyFallback(t *testing.T) {
	c := newStripeCapture(t)
	capRun(t, &StripeNode{}, workflow.NodeInput{}, map[string]interface{}{"api_key": "sk_legacy", "operation": "list_products"})
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_legacy:"))
	if got := c.only(t).Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestStripeNewOperationValidation(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   []string
	}{
		{"refund without target", map[string]interface{}{"operation": "create_refund"}, []string{"payment_intent_id", "charge_id"}},
		{"refund with both targets", map[string]interface{}{"operation": "create_refund", "payment_intent_id": "pi", "charge_id": "ch"}, []string{"not both"}},
		{"refund bad reason", map[string]interface{}{"operation": "create_refund", "charge_id": "ch", "reason": "because"}, []string{"reason", "because"}},
		{"get_payment_intent without id", map[string]interface{}{"operation": "get_payment_intent"}, []string{"payment_intent_id"}},
		{"checkout without success_url", map[string]interface{}{"operation": "create_checkout_session", "line_items": `[{"price":"p"}]`}, []string{"success_url"}},
		{"checkout bad mode", map[string]interface{}{"operation": "create_checkout_session", "mode": "donate", "success_url": "u"}, []string{"mode", "donate"}},
		{"checkout payment without line_items", map[string]interface{}{"operation": "create_checkout_session", "success_url": "u"}, []string{"line_items"}},
		{"checkout bad JSON", map[string]interface{}{"operation": "create_checkout_session", "success_url": "u", "line_items": "[{"}, []string{"line_items"}},
		{"checkout missing price", map[string]interface{}{"operation": "create_checkout_session", "success_url": "u", "line_items": `[{"quantity":1}]`}, []string{"line_items[0].price"}},
		{"checkout zero quantity", map[string]interface{}{"operation": "create_checkout_session", "success_url": "u", "line_items": `[{"price":"p","quantity":0}]`}, []string{"line_items[0].quantity"}},
		{"checkout non-numeric quantity", map[string]interface{}{"operation": "create_checkout_session", "success_url": "u", "line_items": `[{"price":"p","quantity":"two"}]`}, []string{"quantity", "two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newStripeCapture(t)
			err := capRunErr(t, &StripeNode{}, capMerge(map[string]interface{}{"secret_key": "sk"}, tc.config))
			capWantErr(t, err, tc.want...)
			if n := len(c.all()); n != 0 {
				t.Errorf("sent %d requests for an invalid config, want 0", n)
			}
		})
	}
}

func TestStripeHTTPErrorSurfacesStatus(t *testing.T) {
	c := newAPICapture(t, func(*http.Request) (int, string) {
		return http.StatusPaymentRequired, `{"error":{"message":"card declined"}}`
	})
	c.point(t, &stripeBaseURL, "/v1")
	err := capRunErr(t, &StripeNode{}, map[string]interface{}{"secret_key": "sk", "operation": "create_refund", "charge_id": "ch"})
	capWantErr(t, err, "402", "card declined")
}
