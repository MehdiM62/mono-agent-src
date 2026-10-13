package data

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
)

// A best-effort RTF reader: it recovers the document text and, when the
// document holds a table (\cell / \row), its cells. Formatting, pictures,
// fields' instructions and the other non-text destinations are skipped.

// rtfDoc is what parseRTF recovers.
type rtfDoc struct {
	Text string
	Rows [][]string
}

// rtfSkipDestinations are groups whose content is not document text.
var rtfSkipDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true,
	"pict": true, "object": true, "header": true, "footer": true,
	"headerl": true, "headerr": true, "footerl": true, "footerr": true,
	"fldinst": true, "themedata": true, "colorschememapping": true,
	"latentstyles": true, "datastore": true, "xmlnstbl": true,
	"listtable": true, "listoverridetable": true, "rsidtbl": true,
	"generator": true, "filetbl": true, "revtbl": true, "pgdsctbl": true,
}

type rtfState struct {
	skip    bool
	ucSkip  int
	codepag *charmap.Charmap
}

func parseRTF(src string) rtfDoc {
	var (
		doc      rtfDoc
		text     strings.Builder
		cell     strings.Builder
		row      []string
		inTable  bool
		stack    []rtfState
		st       = rtfState{ucSkip: 1, codepag: charmap.Windows1252}
		pendingU []uint16
		skipN    int // fallback characters still to drop after \uN
	)
	emit := func(s string) {
		if st.skip {
			return
		}
		if inTable {
			cell.WriteString(s)
		} else {
			text.WriteString(s)
		}
	}
	flushU := func() {
		if len(pendingU) > 0 {
			emit(string(utf16.Decode(pendingU)))
			pendingU = pendingU[:0]
		}
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '{':
			flushU()
			stack = append(stack, st)
			// {\*\dest …} is an ignorable destination.
			if strings.HasPrefix(src[i+1:], `\*`) {
				st.skip = true
			}
		case '}':
			flushU()
			if len(stack) > 0 {
				st = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case '\r', '\n':
		case '\\':
			if i+1 >= len(src) {
				break
			}
			n := src[i+1]
			switch {
			case n == '\\' || n == '{' || n == '}':
				flushU()
				if skipN > 0 {
					skipN--
				} else {
					emit(string(n))
				}
				i++
			case n == '\'':
				if i+3 < len(src) {
					if b, err := strconv.ParseUint(src[i+2:i+4], 16, 8); err == nil {
						if skipN > 0 {
							skipN--
						} else {
							flushU()
							emit(string(st.codepag.DecodeByte(byte(b))))
						}
					}
				}
				i += 3
			case n == '~':
				emit(" ")
				i++
			case n == '_':
				emit("-")
				i++
			case n == '-' || n == '*' || n == ':' || n == '|':
				i++
			case n == '\r' || n == '\n':
				flushU()
				emit("\n")
				i++
			case isASCIILetter(n):
				j := i + 1
				for j < len(src) && isASCIILetter(src[j]) {
					j++
				}
				word := src[i+1 : j]
				k := j
				if k < len(src) && (src[k] == '-' || (src[k] >= '0' && src[k] <= '9')) {
					k++
					for k < len(src) && src[k] >= '0' && src[k] <= '9' {
						k++
					}
				}
				param, hasParam := 0, k > j
				if hasParam {
					param, _ = strconv.Atoi(src[j:k])
				}
				if k < len(src) && src[k] == ' ' {
					k++
				}
				i = k - 1
				if word != "u" {
					flushU()
				}
				switch word {
				case "u":
					if param < 0 {
						param += 65536
					}
					if !st.skip {
						pendingU = append(pendingU, uint16(param))
					}
					skipN = st.ucSkip
					continue
				case "uc":
					st.ucSkip = param
				case "ansicpg":
					if cm := rtfCodepage(param); cm != nil {
						st.codepag = cm
					}
				case "par", "line", "sect", "page":
					emit("\n")
				case "tab":
					emit("\t")
				case "emdash":
					emit("—")
				case "endash":
					emit("–")
				case "bullet":
					emit("•")
				case "lquote":
					emit("‘")
				case "rquote":
					emit("’")
				case "ldblquote":
					emit("“")
				case "rdblquote":
					emit("”")
				case "intbl":
					inTable = true
				case "cell":
					if !st.skip {
						row = append(row, strings.TrimSpace(cell.String()))
						cell.Reset()
					}
				case "row":
					if !st.skip {
						doc.Rows = append(doc.Rows, row)
						row = nil
						inTable = false
					}
				case "pard":
					inTable = false
				default:
					if rtfSkipDestinations[word] {
						st.skip = true
					}
				}
				skipN = 0
			default:
				i++
			}
		default:
			if skipN > 0 {
				skipN--
				continue
			}
			flushU()
			emit(src[i : i+1])
		}
	}
	flushU()
	if s := strings.TrimSpace(cell.String()); s != "" {
		row = append(row, s)
	}
	if len(row) > 0 {
		doc.Rows = append(doc.Rows, row)
	}
	doc.Text = strings.TrimSpace(text.String())
	return doc
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func rtfCodepage(cp int) *charmap.Charmap {
	switch cp {
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	}
	return nil
}
