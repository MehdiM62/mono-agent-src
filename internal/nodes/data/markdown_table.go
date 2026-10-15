package data

import (
	"strings"

	"golang.org/x/net/html"
)

// table renders an HTML table as a GFM pipe table. The first row is the
// header row (GFM needs one); cells are flattened to one line.
func (c *mdConverter) table(w *textWriter, n *html.Node) {
	var rows [][]string
	var caption string
	var collect func(*html.Node)
	collect = func(x *html.Node) {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			switch ch.Data {
			case "caption":
				caption = strings.Join(strings.Fields(c.render(ch, mdContext{inTable: true})), " ")
			case "thead", "tbody", "tfoot":
				collect(ch)
			case "tr":
				rows = append(rows, c.tableRow(ch))
			}
		}
	}
	collect(n)
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return
	}
	var b strings.Builder
	if caption != "" {
		b.WriteString(caption + "\n\n")
	}
	writeRow := func(r []string) {
		b.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			b.WriteString(" " + cell + " |")
		}
		b.WriteByte('\n')
	}
	writeRow(rows[0])
	b.WriteString("|")
	for i := 0; i < cols; i++ {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')
	for _, r := range rows[1:] {
		writeRow(r)
	}
	w.block(strings.TrimRight(b.String(), "\n"), 2, 2)
}

func (c *mdConverter) tableRow(tr *html.Node) []string {
	var cells []string
	for td := tr.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
			continue
		}
		text := c.render(td, mdContext{inTable: true})
		text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
		cells = append(cells, strings.ReplaceAll(text, "|", `\|`))
	}
	return cells
}
