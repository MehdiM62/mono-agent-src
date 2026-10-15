package data

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/monoes/mono-agent/internal/xlsx"
)

// Spreadsheet-shaped extraction: the file becomes a grid of cells, the grid
// becomes one item per row (see rowsToSheetItems).

func extractTable(op string, data []byte, config map[string]interface{}) ([]workflow.Item, error) {
	maxRows, err := cfgInt(config, "max_rows", -1)
	if err != nil {
		return nil, err
	}
	opts := sheetReadOptions{
		HeaderRow:         cfgBool(config, "header_row", true),
		IncludeEmptyCells: cfgBool(config, "include_empty_cells", false),
		TypedValues:       true,
		MaxRows:           maxRows,
	}
	asText := cfgBool(config, "raw_data", false) || cfgBool(config, "read_as_string", false)
	sheet := cfgString(config, "sheet_name", "")

	var grid [][]interface{}
	switch op {
	case "csv":
		grid, err = readCSVGrid(data, config)
	case "html":
		grid, err = readHTMLGrid(data)
	case "rtf":
		doc := parseRTF(string(data))
		if len(doc.Rows) == 0 {
			// No table: the document's text is the one value there is.
			return []workflow.Item{workflow.NewItem(map[string]interface{}{"text": doc.Text})}, nil
		}
		grid = stringGrid(doc.Rows)
	case "xlsx":
		grid, err = readXLSXGrid(data, sheet, asText)
	case "ods":
		grid, err = readODSTable(data, sheet, asText)
	}
	if err != nil {
		return nil, err
	}
	if op == "xlsx" || op == "ods" || op == "html" {
		if grid, err = applyRange(grid, cfgString(config, "range", "")); err != nil {
			return nil, err
		}
	}
	return rowsToSheetItems(grid, opts), nil
}

func readCSVGrid(data []byte, config map[string]interface{}) ([][]interface{}, error) {
	text, err := decodeText(data, cfgString(config, "encoding", ""), cfgBool(config, "strip_bom", true))
	if err != nil {
		return nil, fmt.Errorf("encoding: %w", err)
	}
	delim, err := csvDelimiter(cfgString(config, "delimiter", ","))
	if err != nil {
		return nil, err
	}
	fromLine, err := cfgInt(config, "from_line", 0)
	if err != nil {
		return nil, err
	}
	// from_line is 1-based like the CSV parser n8n uses; 0 and 1 both mean
	// "from the first line".
	for skip := fromLine - 1; skip > 0 && text != ""; skip-- {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			text = ""
			break
		}
		text = text[i+1:]
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.LazyQuotes = cfgBool(config, "relax_quotes", false)
	r.FieldsPerRecord = -1
	var grid [][]interface{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		row := make([]interface{}, len(rec))
		for i, c := range rec {
			row[i] = c
		}
		grid = append(grid, row)
	}
	return grid, nil
}

// readHTMLGrid reads the first <table> of an HTML document.
func readHTMLGrid(data []byte) ([][]interface{}, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("html: %w", err)
	}
	table := doc.Find("table").First()
	if table.Length() == 0 {
		return nil, errors.New("html: the file has no <table>")
	}
	var grid [][]interface{}
	table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		// Rows of a nested table belong to that table, not this one.
		if tr.Closest("table").Get(0) != table.Get(0) {
			return
		}
		var row []interface{}
		tr.ChildrenFiltered("td, th").Each(func(_ int, c *goquery.Selection) {
			row = append(row, strings.TrimSpace(c.Text()))
		})
		grid = append(grid, row)
	})
	return grid, nil
}

// readXLSXGrid reads one sheet with the repo's xlsx reader, which opens
// files by path, so the bytes go through a temporary file.
func readXLSXGrid(data []byte, sheet string, asText bool) ([][]interface{}, error) {
	tmp, err := os.CreateTemp("", "monoagent-extract-*.xlsx")
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	f, err := xlsx.OpenFile(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("not an xlsx file: %w", err)
	}
	defer f.Close()
	names := f.SheetNames()
	if len(names) == 0 {
		return nil, errors.New("xlsx: the workbook has no sheets")
	}
	if sheet == "" {
		sheet = names[0]
	} else if !containsString(names, sheet) {
		return nil, fmt.Errorf("sheet_name: the workbook has no sheet %q (sheets: %s)", sheet, strings.Join(names, ", "))
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	grid := stringGrid(rows)
	if !asText {
		for _, row := range grid {
			for i, c := range row {
				if n, ok := numericText(c.(string)); ok {
					row[i] = n
				}
			}
		}
	}
	return grid, nil
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

var a1Ref = regexp.MustCompile(`^([A-Za-z]{1,3})([0-9]{1,7})$`)

// applyRange crops a grid. A number is the 0-based row to start at (the
// header is read from it); "A1:C10" or "B2" (to the end) is an A1 range.
func applyRange(grid [][]interface{}, rng string) ([][]interface{}, error) {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return grid, nil
	}
	if n, err := strconv.Atoi(rng); err == nil {
		if n < 0 {
			return nil, fmt.Errorf("range: %d is negative", n)
		}
		if n >= len(grid) {
			return nil, nil
		}
		return grid[n:], nil
	}
	from, to, hasTo := strings.Cut(rng, ":")
	c1, r1, err := parseA1(from)
	if err != nil {
		return nil, err
	}
	c2, r2 := -1, -1
	if hasTo {
		if c2, r2, err = parseA1(to); err != nil {
			return nil, err
		}
		if c2 < c1 || r2 < r1 {
			return nil, fmt.Errorf("range: %q ends before it starts", rng)
		}
	}
	var out [][]interface{}
	for r := r1; r < len(grid) && (r2 < 0 || r <= r2); r++ {
		row := grid[r]
		var cut []interface{}
		for c := c1; c2 < 0 || c <= c2; c++ {
			if c >= len(row) {
				if c2 < 0 {
					break
				}
				cut = append(cut, "")
				continue
			}
			cut = append(cut, row[c])
		}
		out = append(out, cut)
	}
	return out, nil
}

// parseA1 returns the 0-based column and row of an A1 reference.
func parseA1(ref string) (int, int, error) {
	m := a1Ref.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return 0, 0, fmt.Errorf("range: %q is not a row number or an A1 reference like B2:D10", ref)
	}
	col := 0
	for _, ch := range strings.ToUpper(m[1]) {
		col = col*26 + int(ch-'A'+1)
	}
	row, _ := strconv.Atoi(m[2])
	if row < 1 {
		return 0, 0, fmt.Errorf("range: %q: rows start at 1", ref)
	}
	return col - 1, row - 1, nil
}
