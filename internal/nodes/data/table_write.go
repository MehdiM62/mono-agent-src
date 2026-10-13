package data

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/monoes/mono-agent/internal/xlsx"
)

// Writers for the spreadsheet-shaped formats of data.convert_to_file. Each
// takes the rows to write (header row already prepended when wanted) and
// returns the file bytes.

// csvDelimiter parses the delimiter option: one character, with "\t" (as
// typed into a text field) meaning a tab.
func csvDelimiter(s string) (rune, error) {
	switch s {
	case "":
		return ',', nil
	case `\t`, "tab":
		return '\t', nil
	}
	if utf8.RuneCountInString(s) != 1 {
		return 0, fmt.Errorf("delimiter: %q must be a single character", s)
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == '"' || r == '\r' || r == '\n' || r == utf8.RuneError {
		return 0, fmt.Errorf("delimiter: %q cannot be used as a delimiter", s)
	}
	return r, nil
}

func writeCSVTable(rows [][]interface{}, delim rune) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = delim
	for _, row := range rows {
		rec := make([]string, len(row))
		for i, c := range row {
			rec[i] = cellText(c)
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// writeHTMLTable renders rows as a standalone HTML page with one table; the
// header row, when present, goes in <thead> as <th> cells.
func writeHTMLTable(rows [][]interface{}, headerRow bool, title string) []byte {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title></head><body>\n<table>\n")
	writeRow := func(row []interface{}, tag string) {
		b.WriteString("<tr>")
		for _, c := range row {
			b.WriteString("<" + tag + ">" + html.EscapeString(cellText(c)) + "</" + tag + ">")
		}
		b.WriteString("</tr>\n")
	}
	if headerRow && len(rows) > 0 {
		b.WriteString("<thead>\n")
		writeRow(rows[0], "th")
		b.WriteString("</thead>\n")
		rows = rows[1:]
	}
	b.WriteString("<tbody>\n")
	for _, row := range rows {
		writeRow(row, "td")
	}
	b.WriteString("</tbody>\n</table>\n</body></html>\n")
	return []byte(b.String())
}

// writeRTFTable renders rows as an RTF table (header row bold).
func writeRTFTable(rows [][]interface{}, headerRow bool) []byte {
	var b strings.Builder
	b.WriteString(`{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0\fswiss Calibri;}}\f0\fs22` + "\n")
	for i, row := range rows {
		b.WriteString(`\trowd\trgaph108`)
		for c := range row {
			fmt.Fprintf(&b, `\cellx%d`, (c+1)*2000)
		}
		b.WriteString("\n")
		for _, c := range row {
			b.WriteString(`\pard\intbl `)
			text := rtfEscape(cellText(c))
			if i == 0 && headerRow {
				text = `{\b ` + text + `}`
			}
			b.WriteString(text + `\cell` + "\n")
		}
		b.WriteString(`\row` + "\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// rtfEscape escapes RTF syntax characters and writes non-ASCII characters as
// \uN? escapes (UTF-16 code units, signed, with '?' as the fallback).
func rtfEscape(s string) string {
	var b strings.Builder
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\line `)
		case r == '\t':
			b.WriteString(`\tab `)
		case r < 0x20:
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, `\u%d?`, int16(uint16(r)))
		default:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%d?\u%d?`, int16(uint16(0xD800+(r>>10))), int16(uint16(0xDC00+(r&0x3FF))))
		}
	}
	return b.String()
}

// writeXLSXTable builds an .xlsx workbook with the repo's own writer.
// Without compression the parts are re-packed uncompressed (n8n's default).
func writeXLSXTable(rows [][]interface{}, sheet string, compress bool) ([]byte, error) {
	anyRows := make([][]any, len(rows))
	for i, r := range rows {
		row := make([]any, len(r))
		for j, c := range r {
			if f, ok := cellNumber(c); ok {
				row[j] = f
			} else if c == nil {
				row[j] = nil
			} else {
				row[j] = cellText(c)
			}
		}
		anyRows[i] = row
	}
	out, err := xlsx.Marshal(sheet, anyRows)
	if err != nil || compress {
		return out, err
	}
	return repackZip(out, zip.Store)
}

// repackZip rewrites every entry of a zip archive with the given method,
// keeping entry order.
func repackZip(data []byte, method uint16) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: method, Modified: f.Modified})
		if err == nil {
			_, err = io.Copy(w, rc)
		}
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
