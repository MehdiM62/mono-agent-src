package data

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"

	"github.com/monoes/mono-agent/internal/workflow"
)

// PDF text extraction for data.extract_from_file, with the pure-Go
// github.com/ledongthuc/pdf reader. Output follows n8n's shape: numpages,
// numrender, info, and text (one string, or one string per page when
// join_pages is off).

// pdfInfoKeys are the document information entries copied into "info".
var pdfInfoKeys = []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate", "ModDate", "Trapped"}

func extractPDFItem(item workflow.Item, prop string, data []byte, config map[string]interface{}) ([]workflow.Item, error) {
	keep, err := keepSourceMode(config)
	if err != nil {
		return nil, err
	}
	maxPages, err := cfgInt(config, "max_pages", 0)
	if err != nil {
		return nil, err
	}
	if maxPages < 0 {
		return nil, fmt.Errorf("max_pages: %d is negative", maxPages)
	}
	res, err := extractPDF(data, cfgString(config, "password", ""), maxPages, cfgBool(config, "join_pages", true))
	if err != nil {
		return nil, err
	}
	out := sourceBase(item, prop, keep)
	for k, v := range res {
		out.JSON[k] = v
	}
	return []workflow.Item{out}, nil
}

// extractPDF reads the text of up to maxPages pages (0 = all).
func extractPDF(data []byte, password string, maxPages int, joinPages bool) (res map[string]interface{}, err error) {
	// The reader panics on some malformed input; report that as an error.
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("pdf: malformed PDF: %v", r)
		}
	}()
	var pw func() string
	if password != "" {
		tried := false
		pw = func() string {
			if tried {
				return ""
			}
			tried = true
			return password
		}
	}
	r, err := pdf.NewReaderEncrypted(bytes.NewReader(data), int64(len(data)), pw)
	if err != nil {
		if errors.Is(err, pdf.ErrInvalidPassword) {
			if password == "" {
				return nil, errors.New("password: the PDF is encrypted; set password")
			}
			return nil, errors.New("password: the PDF is encrypted and the password is wrong")
		}
		return nil, fmt.Errorf("not a readable PDF: %w", err)
	}
	num := r.NumPage()
	toRead := num
	if maxPages > 0 && maxPages < num {
		toRead = maxPages
	}
	var pages []string
	total := 0
	for i := 1; i <= toRead; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			// The page tree ends before the page count it declares.
			break
		}
		// nil: fonts are looked up per page, since two pages may use the
		// same resource name for different fonts.
		text, err := p.GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("pdf: page %d: %w", i, err)
		}
		text = strings.TrimSpace(text)
		total += len(text)
		if total > maxBinaryBytes {
			return nil, fmt.Errorf("pdf: extracted text exceeds %d bytes", maxBinaryBytes)
		}
		pages = append(pages, text)
	}

	info := map[string]interface{}{}
	if len(data) >= 8 {
		info["PDFFormatVersion"] = string(data[5:8])
	}
	info["IsEncrypted"] = !r.Trailer().Key("Encrypt").IsNull()
	infoDict := r.Trailer().Key("Info")
	for _, k := range pdfInfoKeys {
		if v := infoDict.Key(k); !v.IsNull() {
			if s := v.Text(); s != "" {
				info[k] = s
			} else if n := v.Name(); n != "" {
				info[k] = n
			}
		}
	}

	res = map[string]interface{}{"numpages": num, "numrender": num, "info": info}
	if joinPages {
		res["text"] = strings.Join(pages, "\n\n")
	} else {
		list := make([]interface{}, len(pages))
		for i, p := range pages {
			list[i] = p
		}
		res["text"] = list
	}
	return res, nil
}
