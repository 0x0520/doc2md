package htmldoc

import (
	"bytes"
	"regexp"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// xmlDeclEncodingRe reads the encoding of an XHTML document from its XML
// declaration, which DetermineEncoding (a <meta> prescan) does not look at.
var xmlDeclEncodingRe = regexp.MustCompile(`^\s*<\?xml[^>]*?\sencoding\s*=\s*["']([^"']+)["']`)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// toUTF8 transcodes a document to UTF-8 from its BOM, XML declaration or
// <meta> charset; the parser itself assumes UTF-8 and would turn every other
// byte into U+FFFD.
func toUTF8(data []byte) []byte {
	if rest, ok := bytes.CutPrefix(data, utf8BOM); ok {
		return rest
	}
	e, name, certain := charset.DetermineEncoding(data, "")
	if m := xmlDeclEncodingRe.FindSubmatch(data[:min(len(data), 1024)]); m != nil && !certain {
		if xe, xn := charset.Lookup(string(m[1])); xe != nil {
			e, name, certain = xe, xn, true
			// Without a BOM a "utf-16" declaration cannot be true of bytes that
			// start with ASCII markup.
			if xn == "utf-16le" || xn == "utf-16be" {
				name = "utf-8"
			}
		}
	}
	// DetermineEncoding falls back to windows-1252 when the first KB declares
	// nothing; text that is valid UTF-8 beyond that is far more likely UTF-8
	// than a mislabelled legacy page.
	if name == "utf-8" || (!certain && utf8.Valid(data)) {
		return data
	}
	out, err := e.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(out, utf8BOM)
}
