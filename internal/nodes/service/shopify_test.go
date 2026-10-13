package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// shopifyTestResponse carries every key the Shopify operations unwrap.
const shopifyTestResponse = `{
  "product": {"id": 1}, "order": {"id": 1}, "customer": {"id": 1},
  "products": [{"id": 1}], "orders": [{"id": 1}], "customers": [{"id": 1}]
}`

// newShopifyCapture points shopifyBaseURLFor at an httptest server and
// records the shop name it was asked to resolve.
func newShopifyCapture(t *testing.T) (*apiCapture, *string) {
	t.Helper()
	c := newAPICapture(t, func(*http.Request) (int, string) { return http.StatusOK, shopifyTestResponse })
	var gotShop string
	orig := shopifyBaseURLFor
	shopifyBaseURLFor = func(shop string) string {
		gotShop = shop
		return c.srv.URL
	}
	t.Cleanup(func() { shopifyBaseURLFor = orig })
	return c, &gotShop
}

func TestShopifyOperationsRequests(t *testing.T) {
	const v = "/admin/api/" + shopifyDefaultAPIVersion
	cases := []struct {
		name   string
		config map[string]interface{}
		method string
		path   string
		query  map[string]string
		body   map[string]interface{} // nil = no body expected
	}{
		{"list_products", map[string]interface{}{"operation": "list_products"},
			"GET", v + "/products.json", map[string]string{"limit": "50"}, nil},
		{"get_product", map[string]interface{}{"operation": "get_product", "product_id": "11"},
			"GET", v + "/products/11.json", nil, nil},
		{"create_product", map[string]interface{}{"operation": "create_product", "title": "Mug", "vendor": "Acme", "product_type": "Kitchen"},
			"POST", v + "/products.json", nil,
			map[string]interface{}{"product": map[string]interface{}{"title": "Mug", "vendor": "Acme", "product_type": "Kitchen"}}},
		{"update_product", map[string]interface{}{"operation": "update_product", "product_id": "11", "title": "Big Mug"},
			"PUT", v + "/products/11.json", nil,
			map[string]interface{}{"product": map[string]interface{}{"id": "11", "title": "Big Mug"}}},
		{"list_orders", map[string]interface{}{"operation": "list_orders", "limit": float64(5), "status": "open"},
			"GET", v + "/orders.json", map[string]string{"limit": "5", "status": "open"}, nil},
		{"get_order", map[string]interface{}{"operation": "get_order", "order_id": "22"},
			"GET", v + "/orders/22.json", nil, nil},
		{"update_order", map[string]interface{}{"operation": "update_order", "order_id": "22", "status": "paid"},
			"PUT", v + "/orders/22.json", nil,
			map[string]interface{}{"order": map[string]interface{}{"id": "22", "financial_status": "paid"}}},
		{"list_customers", map[string]interface{}{"operation": "list_customers", "limit": float64(2)},
			"GET", v + "/customers.json", map[string]string{"limit": "2"}, nil},
		{"get_customer", map[string]interface{}{"operation": "get_customer", "customer_id": "33"},
			"GET", v + "/customers/33.json", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, gotShop := newShopifyCapture(t)
			cfg := capMerge(map[string]interface{}{"shop_domain": "mystore.myshopify.com", "access_token": "shpat_1"}, tc.config)
			items := capRun(t, &ShopifyNode{}, workflow.NodeInput{}, cfg)
			if len(items) != 1 {
				t.Errorf("got %d items, want 1", len(items))
			}
			if *gotShop != "mystore" {
				t.Errorf("shop resolved as %q, want mystore", *gotShop)
			}
			req := c.only(t)
			if req.Method != tc.method || req.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.method, tc.path)
			}
			if got := req.Header.Get("X-Shopify-Access-Token"); got != "shpat_1" {
				t.Errorf("X-Shopify-Access-Token = %q, want shpat_1", got)
			}
			capWantValues(t, "query", req.Query, tc.query)
			if tc.body == nil {
				if len(req.Body) != 0 {
					t.Errorf("body = %q, want none", req.Body)
				}
				return
			}
			if ct := req.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if got := req.json(t); !reflect.DeepEqual(got, tc.body) {
				t.Errorf("body = %v, want %v", got, tc.body)
			}
		})
	}
}

func TestShopifyAPIVersion(t *testing.T) {
	cases := []struct {
		version  interface{}
		wantPath string
	}{
		{nil, "/admin/api/2026-04/products.json"},
		{"", "/admin/api/2026-04/products.json"},
		{"2025-10", "/admin/api/2025-10/products.json"},
		{" 2024-07 ", "/admin/api/2024-07/products.json"},
		{"unstable", "/admin/api/unstable/products.json"},
	}
	for _, tc := range cases {
		c, _ := newShopifyCapture(t)
		cfg := map[string]interface{}{"shop_domain": "mystore", "access_token": "tok", "operation": "list_products"}
		if tc.version != nil {
			cfg["api_version"] = tc.version
		}
		capRun(t, &ShopifyNode{}, workflow.NodeInput{}, cfg)
		if got := c.only(t).Path; got != tc.wantPath {
			t.Errorf("api_version %v: path = %q, want %q", tc.version, got, tc.wantPath)
		}
	}
}

func TestShopifyInvalidAPIVersion(t *testing.T) {
	for _, bad := range []string{"2024-1", "2024-13", "v2024-01", "latest", "2024-01/../x", "24-01"} {
		c, _ := newShopifyCapture(t)
		err := capRunErr(t, &ShopifyNode{}, map[string]interface{}{
			"shop_domain": "mystore", "access_token": "tok", "operation": "list_products", "api_version": bad,
		})
		capWantErr(t, err, "api_version", bad)
		if n := len(c.all()); n != 0 {
			t.Errorf("api_version %q: sent %d requests, want 0", bad, n)
		}
	}
}

func TestShopifyRejectsHostEscapingShop(t *testing.T) {
	c, _ := newShopifyCapture(t)
	err := capRunErr(t, &ShopifyNode{}, map[string]interface{}{
		"shop_domain": "evil.com/x?", "access_token": "tok", "operation": "list_products",
	})
	capWantErr(t, err, "shop_domain")
	if n := len(c.all()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

func TestShopifyDefaultBaseURL(t *testing.T) {
	if got := shopifyBaseURLFor("mystore"); got != "https://mystore.myshopify.com" {
		t.Errorf("shopifyBaseURLFor = %q", got)
	}
}

// TestAPIVersionSchemaDefaultsMatchCode keeps the schema's displayed default
// in step with the const the node actually falls back to.
func TestAPIVersionSchemaDefaultsMatchCode(t *testing.T) {
	for file, want := range map[string]string{
		"service.shopify.json":    shopifyDefaultAPIVersion,
		"service.salesforce.json": salesforceDefaultAPIVersion,
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "workflow", "schemas", file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var schema struct {
			Fields []struct {
				Key     string      `json:"key"`
				Default interface{} `json:"default"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		found := false
		for _, f := range schema.Fields {
			if f.Key == "api_version" {
				found = true
				if f.Default != want {
					t.Errorf("%s api_version default = %v, want %q", file, f.Default, want)
				}
			}
		}
		if !found {
			t.Errorf("%s has no api_version field", file)
		}
	}
}
