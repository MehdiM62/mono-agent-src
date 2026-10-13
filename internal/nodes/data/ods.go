package data

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

// OpenDocument Spreadsheet (.ods) reading and writing, written against the
// ODF 1.2 package layout: a zip whose first entry is an uncompressed
// "mimetype", a META-INF/manifest.xml, and the sheet data in content.xml.

const (
	odsMime = "application/vnd.oasis.opendocument.spreadsheet"

	nsODFOffice = "urn:oasis:names:tc:opendocument:xmlns:office:1.0"
	nsODFTable  = "urn:oasis:names:tc:opendocument:xmlns:table:1.0"
	nsODFText   = "urn:oasis:names:tc:opendocument:xmlns:text:1.0"

	// maxODSCells bounds the cells one sheet may expand to, including
	// repeated rows and columns, so a tiny file declaring a million repeats
	// of a non-empty cell cannot exhaust memory.
	maxODSCells = 8 << 20
	// maxODSEntries bounds the members of the zip container we index.
	maxODSEntries = 8192
)

// ── write ─────────────────────────────────────────────────────────────────────

func writeODSTable(rows [][]interface{}, sheet string, compress bool) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)

	// "mimetype" first, stored, without a data descriptor, so the media type
	// sits at a fixed offset where file-type sniffers look for it.
	mt := []byte(odsMime)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: "mimetype", Method: zip.Store,
		CRC32: crc32.ChecksumIEEE(mt), CompressedSize64: uint64(len(mt)), UncompressedSize64: uint64(len(mt)),
	})
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(mt); err != nil {
		return nil, err
	}

	method := zip.Store
	if compress {
		method = zip.Deflate
	}
	parts := []struct{ name, body string }{
		{"META-INF/manifest.xml", odsManifest},
		{"meta.xml", odsMeta},
		{"styles.xml", odsStyles},
		{"content.xml", odsContent(rows, sheet)},
	}
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: method})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, p.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

const odsXMLHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

const odsManifest = odsXMLHeader + `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">` +
	`<manifest:file-entry manifest:full-path="/" manifest:version="1.2" manifest:media-type="` + odsMime + `"/>` +
	`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
	`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/>` +
	`<manifest:file-entry manifest:full-path="meta.xml" manifest:media-type="text/xml"/>` +
	`</manifest:manifest>`

const odsMeta = odsXMLHeader + `<office:document-meta xmlns:office="` + nsODFOffice + `" xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0" office:version="1.2">` +
	`<office:meta><meta:generator>mono-agent</meta:generator></office:meta></office:document-meta>`

const odsStyles = odsXMLHeader + `<office:document-styles xmlns:office="` + nsODFOffice + `" office:version="1.2"/>`

func odsContent(rows [][]interface{}, sheet string) string {
	var b strings.Builder
	b.WriteString(odsXMLHeader)
	b.WriteString(`<office:document-content xmlns:office="` + nsODFOffice + `" xmlns:table="` + nsODFTable + `" xmlns:text="` + nsODFText + `" office:version="1.2">`)
	b.WriteString(`<office:body><office:spreadsheet><table:table table:name="` + xmlAttr(sheet) + `">`)
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols > 0 {
		fmt.Fprintf(&b, `<table:table-column table:number-columns-repeated="%d"/>`, cols)
	}
	for _, row := range rows {
		b.WriteString("<table:table-row>")
		for _, c := range row {
			writeODSCell(&b, c)
		}
		b.WriteString("</table:table-row>")
	}
	b.WriteString(`</table:table></office:spreadsheet></office:body></office:document-content>`)
	return b.String()
}

func writeODSCell(b *strings.Builder, c interface{}) {
	if f, ok := cellNumber(c); ok {
		v := strconv.FormatFloat(f, 'f', -1, 64)
		b.WriteString(`<table:table-cell office:value-type="float" office:value="` + v + `"><text:p>` + v + `</text:p></table:table-cell>`)
		return
	}
	if v, ok := c.(bool); ok {
		b.WriteString(`<table:table-cell office:value-type="boolean" office:boolean-value="` + strconv.FormatBool(v) + `"><text:p>` + strings.ToUpper(strconv.FormatBool(v)) + `</text:p></table:table-cell>`)
		return
	}
	s := cellText(c)
	if s == "" {
		b.WriteString(`<table:table-cell/>`)
		return
	}
	b.WriteString(`<table:table-cell office:value-type="string">`)
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		b.WriteString("<text:p>" + odfText(line) + "</text:p>")
	}
	b.WriteString(`</table:table-cell>`)
}

// odfText escapes one paragraph of text. ODF collapses runs of spaces, so
// every space after the first of a run (and a leading one) is written as
// <text:s/>, and tabs as <text:tab/>.
func odfText(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		switch {
		case r == ' ' && prevSpace:
			b.WriteString(`<text:s/>`)
		case r == ' ':
			b.WriteByte(' ')
			prevSpace = true
			continue
		case r == '\t':
			b.WriteString(`<text:tab/>`)
		case !validXMLChar(r):
		default:
			xml.EscapeText(&b, []byte(string(r)))
		}
		prevSpace = false
	}
	return b.String()
}

func xmlAttr(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func validXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

// ── read ──────────────────────────────────────────────────────────────────────

// readODSTable returns the cells of one sheet (the first when sheet is "").
// asText returns every cell as its displayed text; otherwise numeric and
// boolean cells are float64 / bool.
func readODSTable(data []byte, sheet string, asText bool) ([][]interface{}, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not an ODS file: %w", err)
	}
	if len(zr.File) > maxODSEntries {
		return nil, fmt.Errorf("ods: container has %d entries, limit is %d", len(zr.File), maxODSEntries)
	}
	var content *zip.File
	for _, f := range zr.File {
		if f.Name == "content.xml" {
			content = f
			break
		}
	}
	if content == nil {
		return nil, errors.New("not an ODS file: content.xml is missing")
	}
	rc, err := content.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	r := &odsReader{d: xml.NewDecoder(io.LimitReader(rc, maxBinaryBytes)), asText: asText}
	return r.sheet(sheet)
}

type odsReader struct {
	d      *xml.Decoder
	asText bool
	cells  int
}

func (r *odsReader) sheet(want string) ([][]interface{}, error) {
	var names []string
	for {
		tok, err := r.d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ods: content.xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != nsODFTable || se.Name.Local != "table" {
			continue
		}
		name := odsAttr(se, nsODFTable, "name")
		names = append(names, name)
		if want != "" && name != want {
			if err := r.d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		return r.table()
	}
	if want == "" {
		return nil, errors.New("ods: the file has no sheets")
	}
	return nil, fmt.Errorf("sheet_name: the file has no sheet %q (sheets: %s)", want, strings.Join(names, ", "))
}

// table reads rows until </table:table>. Repeated empty rows and cells are
// only materialised when something non-empty follows them, because writers
// pad sheets to the full grid with huge repeat counts.
func (r *odsReader) table() ([][]interface{}, error) {
	var rows [][]interface{}
	pendingRows := 0
	for {
		tok, err := r.d.Token()
		if err != nil {
			return nil, fmt.Errorf("ods: content.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Space == nsODFTable && t.Name.Local == "table" {
				return rows, nil
			}
		case xml.StartElement:
			if t.Name.Space != nsODFTable || t.Name.Local != "table-row" {
				continue
			}
			row, err := r.row()
			if err != nil {
				return nil, err
			}
			rep := odsRepeat(t, "number-rows-repeated")
			if len(row) == 0 {
				pendingRows += rep
				continue
			}
			if err := r.charge((pendingRows + rep) * len(row)); err != nil {
				return nil, err
			}
			for ; pendingRows > 0; pendingRows-- {
				rows = append(rows, nil)
			}
			for i := 0; i < rep; i++ {
				rows = append(rows, append([]interface{}(nil), row...))
			}
		}
	}
}

func (r *odsReader) charge(n int) error {
	r.cells += n
	if r.cells > maxODSCells {
		return fmt.Errorf("ods: sheet expands to more than %d cells", maxODSCells)
	}
	return nil
}

func (r *odsReader) row() ([]interface{}, error) {
	var row []interface{}
	pending := 0
	for {
		tok, err := r.d.Token()
		if err != nil {
			return nil, fmt.Errorf("ods: content.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Space == nsODFTable && t.Name.Local == "table-row" {
				return row, nil
			}
		case xml.StartElement:
			if t.Name.Space != nsODFTable || (t.Name.Local != "table-cell" && t.Name.Local != "covered-table-cell") {
				continue
			}
			val, err := r.cell(t)
			if err != nil {
				return nil, err
			}
			rep := odsRepeat(t, "number-columns-repeated")
			if cellText(val) == "" {
				pending += rep
				continue
			}
			if pending+rep > maxODSCells {
				return nil, fmt.Errorf("ods: row expands to more than %d cells", maxODSCells)
			}
			for ; pending > 0; pending-- {
				row = append(row, "")
			}
			for i := 0; i < rep; i++ {
				row = append(row, val)
			}
		}
	}
}

// cell reads one cell's value and consumes it through its end element.
func (r *odsReader) cell(se xml.StartElement) (interface{}, error) {
	var text strings.Builder
	paras := 0
	depth := 1
	for depth > 0 {
		tok, err := r.d.Token()
		if err != nil {
			return nil, fmt.Errorf("ods: content.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space != nsODFText {
				continue
			}
			switch t.Name.Local {
			case "p", "h":
				if paras > 0 {
					text.WriteByte('\n')
				}
				paras++
			case "s":
				n := 1
				if c, err := strconv.Atoi(odsAttr(t, nsODFText, "c")); err == nil && c > 0 && c < 1<<16 {
					n = c
				}
				text.WriteString(strings.Repeat(" ", n))
			case "tab":
				text.WriteByte('\t')
			case "line-break":
				text.WriteByte('\n')
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			text.Write(t)
		}
	}
	shown := text.String()
	if r.asText {
		return shown, nil
	}
	switch odsAttr(se, nsODFOffice, "value-type") {
	case "float", "percentage", "currency":
		if f, err := strconv.ParseFloat(odsAttr(se, nsODFOffice, "value"), 64); err == nil {
			return f, nil
		}
	case "boolean":
		if b, err := strconv.ParseBool(odsAttr(se, nsODFOffice, "boolean-value")); err == nil {
			return b, nil
		}
	case "date":
		if v := odsAttr(se, nsODFOffice, "date-value"); v != "" {
			return v, nil
		}
	case "time":
		if v := odsAttr(se, nsODFOffice, "time-value"); v != "" {
			return v, nil
		}
	}
	return shown, nil
}

func odsAttr(se xml.StartElement, space, local string) string {
	for _, a := range se.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func odsRepeat(se xml.StartElement, attr string) int {
	n, err := strconv.Atoi(odsAttr(se, nsODFTable, attr))
	if err != nil || n < 1 {
		return 1
	}
	return n
}
