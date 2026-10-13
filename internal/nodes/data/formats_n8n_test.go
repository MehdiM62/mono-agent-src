package data

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func one(m map[string]interface{}) []workflow.Item { return []workflow.Item{{JSON: m}} }

func TestMarkdownHTMLToMarkdownFixture(t *testing.T) {
	got := run(t, &MarkdownNode{}, one(map[string]interface{}{"h": fixture(t, "article.html")}),
		map[string]interface{}{"mode": "html_to_markdown", "html": "{{ $json.h }}"})
	if want := strings.TrimRight(fixture(t, "article.md"), "\n"); got[0]["data"] != want {
		t.Errorf("html_to_markdown:\n%s\n--- want ---\n%s", got[0]["data"], want)
	}
	if got[0]["h"] == nil {
		t.Error("input fields must be kept")
	}
}

func TestMarkdownToHTMLModes(t *testing.T) {
	got := run(t, &MarkdownNode{}, one(map[string]interface{}{}), map[string]interface{}{"mode": "markdown_to_html", "markdown": "# Hi\n\n~~x~~\n", "destination_key": "out.html", "strikethrough": true})
	html := got[0]["out"].(map[string]interface{})["html"].(string)
	if !strings.Contains(html, `<h1 id="hi">Hi</h1>`) || !strings.Contains(html, "<del>x</del>") {
		t.Errorf("markdown_to_html: %q", html)
	}
	got = run(t, &MarkdownNode{}, one(map[string]interface{}{"md": "*a*"}), map[string]interface{}{"mode": "markdown_to_html", "field": "md"})
	if got[0]["md"] != "<p><em>a</em></p>" {
		t.Errorf("field source: %v", got[0])
	}
	if _, err := (&MarkdownNode{}).Execute(context.Background(), workflow.NodeInput{Items: one(nil)}, map[string]interface{}{"mode": "rst"}); err == nil {
		t.Error("unknown mode: expected error")
	}
	if _, err := (&MarkdownNode{}).Execute(context.Background(), workflow.NodeInput{Items: one(nil)}, map[string]interface{}{"mode": "html_to_markdown"}); err == nil {
		t.Error("missing html: expected error")
	}
	// The original operation still works.
	got = run(t, &MarkdownNode{}, one(map[string]interface{}{"b": "**x**"}), map[string]interface{}{"field": "b"})
	if got[0]["b"] != "<p><strong>x</strong></p>\n" {
		t.Errorf("legacy: %q", got[0]["b"])
	}
}

func TestXMLToJSONFixture(t *testing.T) {
	got := run(t, &XMLNode{}, one(map[string]interface{}{"data": fixture(t, "catalog.xml")}), map[string]interface{}{"operation": "xml_to_json"})
	cat := got[0]["catalog"].(map[string]interface{})
	books := cat["book"].([]interface{})
	b0 := books[0].(map[string]interface{})
	if b0["id"] != "bk101" || len(b0["author"].([]interface{})) != 2 || b0["price"].(map[string]interface{})["_"] != "44.95" {
		t.Errorf("xml_to_json: %v", b0)
	}
	if books[1].(map[string]interface{})["discontinued"] != "" {
		t.Errorf("empty element: %v", books[1])
	}
	got = run(t, &XMLNode{}, one(map[string]interface{}{"data": fixture(t, "catalog.xml")}), map[string]interface{}{"operation": "xml_to_json", "explicit_root": false, "merge_attrs": false, "trim": true, "normalize": true, "explicit_array": true})
	b1 := got[0]["book"].([]interface{})[0].(map[string]interface{})
	if b1["$"].(map[string]interface{})["id"] != "bk101" {
		t.Errorf("attrs under $: %v", b1)
	}
	if d := b1["description"].([]interface{})[0]; d != "An in-depth look at creating applications with XML & friends." {
		t.Errorf("trim+normalize: %q", d)
	}
	if _, err := (&XMLNode{}).Execute(context.Background(), workflow.NodeInput{Items: one(map[string]interface{}{"data": "<a/>"})}, map[string]interface{}{"operation": "xml_to_json", "attrkey": "__proto__"}); err == nil {
		t.Error("reserved attrkey: expected error")
	}
	if _, err := (&XMLNode{}).Execute(context.Background(), workflow.NodeInput{Items: one(map[string]interface{}{})}, map[string]interface{}{"operation": "xml_to_json"}); err == nil {
		t.Error("missing property: expected error")
	}
}

func TestJSONToXMLRoundTrip(t *testing.T) {
	in := map[string]interface{}{"a": map[string]interface{}{"$": map[string]interface{}{"id": "1"}, "b": []interface{}{"x", "y"}, "_": "t"}}
	got := run(t, &XMLNode{}, one(in), map[string]interface{}{"operation": "json_to_xml"})
	xml := got[0]["data"].(string)
	if !strings.Contains(xml, `<a id="1">`) || strings.Count(xml, "<b>") != 2 {
		t.Errorf("json_to_xml: %s", xml)
	}
	back := run(t, &XMLNode{}, one(map[string]interface{}{"data": xml}), map[string]interface{}{"operation": "xml_to_json", "trim": true})
	a := back[0]["a"].(map[string]interface{})
	if a["id"] != "1" || len(a["b"].([]interface{})) != 2 {
		t.Errorf("round trip: %v", back[0])
	}
	got = run(t, &XMLNode{}, one(map[string]interface{}{"x": 1.0, "y": "z"}), map[string]interface{}{"operation": "json_to_xml", "headless": true, "root_name": "doc", "data_property_name": "out"})
	if s := got[0]["out"].(string); strings.HasPrefix(s, "<?xml") || !strings.HasPrefix(s, "<doc>") {
		t.Errorf("headless/root: %s", s)
	}
}

func TestHTMLExtractContent(t *testing.T) {
	cfg := map[string]interface{}{"operation": "extract_html_content", "data_property_name": "h", "trim_values": true, "clean_up_text": true,
		"extraction_values": `[{"key":"title","css_selector":"h1"},
			{"key":"skus","css_selector":"li.product","return_value":"attribute","attribute":"data-sku","return_array":true},
			{"key":"size","css_selector":"select[name=size]","return_value":"value"},
			{"key":"text","css_selector":"#details","skip_selectors":".ad"},
			{"key":"first","css_selector":"#details p","return_value":"html"}]`}
	got := run(t, &HTMLNode{}, one(map[string]interface{}{"h": fixture(t, "products.html")}), cfg)
	wantJSON(t, got[0]["skus"], `["A1","B2","C3"]`)
	if got[0]["title"] != "Spring Sale" || got[0]["size"] != "m" {
		t.Errorf("extract: %v", got[0])
	}
	if s, _ := got[0]["text"].(string); strings.Contains(s, "Buy now") || !strings.Contains(s, "First paragraph") {
		t.Errorf("skip selectors: %q", s)
	}
	if s, _ := got[0]["first"].(string); !strings.Contains(s, `<a href="https://example.com/more">link</a>`) {
		t.Errorf("html return: %q", s)
	}
}

func TestHTMLTemplateAndTable(t *testing.T) {
	got := run(t, &HTMLNode{}, items(t, `[{"n":"Ada"},{"n":"Alan"}]`), map[string]interface{}{"operation": "generate_html_template", "html": "<p>{{ $json.n }}</p>"})
	if got[0]["html"] != "<p>Ada</p>" || got[1]["html"] != "<p>Alan</p>" {
		t.Errorf("template per item: %v", got)
	}
	got = run(t, &HTMLNode{}, items(t, `[{"name":"a","v":1},{"name":"b","v":2}]`), map[string]interface{}{"operation": "convert_to_html_table", "caption": "Cap", "capitalize": true})
	table := got[0]["table"].(string)
	if len(got) != 1 || !strings.Contains(table, "<caption>Cap</caption>") || !strings.Contains(table, "<th>Name</th>") || strings.Count(table, "<tr>") != 3 {
		t.Errorf("table: %s", table)
	}
	got = run(t, &HTMLNode{}, items(t, `[{"a":1}]`), map[string]interface{}{"operation": "convert_to_html_table", "custom_styling": true, "table_attributes": `class="t"`})
	if !strings.Contains(got[0]["table"].(string), `<table class="t">`) {
		t.Errorf("custom styling: %s", got[0]["table"])
	}
}
