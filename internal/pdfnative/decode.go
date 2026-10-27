package pdfnative

import (
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

const maxFontProgram = 1 << 24

func buildDecoder(font pdf.Font) map[int]string {
	out := map[int]string{}
	v := font.V
	if enc := v.Key("Encoding"); enc.Kind() == pdf.Dict {
		parseDifferences(enc.Key("Differences"), out)
	}
	if tu := v.Key("ToUnicode"); !tu.IsNull() {
		parseToUnicode(tu, out)
	}
	fillEmbeddedLigatures(v, out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func fillEmbeddedLigatures(v pdf.Value, out map[int]string) {
	if v.Key("Subtype").Name() != "Type0" {
		return
	}
	df := v.Key("DescendantFonts")
	if df.Kind() != pdf.Array || df.Len() == 0 {
		return
	}
	d0 := df.Index(0)
	fd := d0.Key("FontDescriptor")
	prog := readFontProgram(fd)
	if len(prog) == 0 {
		return
	}
	byGID := embeddedLigatures(prog)
	if len(byGID) == 0 {
		return
	}
	c2g := d0.Key("CIDToGIDMap")
	if c2g.Name() == "Identity" || c2g.IsNull() {
		for gid, s := range byGID {
			if _, ok := out[gid]; !ok {
				out[gid] = s
			}
		}
		return
	}
	m := readStream(c2g)
	for cid := 0; cid*2+1 < len(m); cid++ {
		gid := int(m[cid*2])<<8 | int(m[cid*2+1])
		if s, ok := byGID[gid]; ok {
			if _, exists := out[cid]; !exists {
				out[cid] = s
			}
		}
	}
}

func readFontProgram(fd pdf.Value) []byte {
	for _, k := range []string{"FontFile2", "FontFile3"} {
		if ff := fd.Key(k); !ff.IsNull() {
			if b := readStream(ff); len(b) > 0 {
				return b
			}
		}
	}
	return nil
}

func readStream(v pdf.Value) []byte {
	defer func() { _ = recover() }()
	r := v.Reader()
	if r == nil {
		return nil
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxFontProgram))
	if err != nil && len(data) == 0 {
		return nil
	}
	return data
}

func parseToUnicode(stream pdf.Value, out map[int]string) {
	defer func() { _ = recover() }()
	r := stream.Reader()
	if r == nil {
		return
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 1<<22))
	if err != nil && len(data) == 0 {
		return
	}
	toks := lexCMap(string(data))

	for i := 0; i < len(toks); i++ {
		switch toks[i] {
		case "beginbfchar":
			for i++; i+1 < len(toks) && toks[i] != "endbfchar"; i += 2 {
				src, ok1 := hexTok(toks[i])
				dst, ok2 := hexStr(toks[i+1])
				if ok1 && ok2 {
					out[src] = dst
				}
			}
		case "beginbfrange":
			for i++; i+2 < len(toks) && toks[i] != "endbfrange"; {
				lo, ok1 := hexTok(toks[i])
				hi, ok2 := hexTok(toks[i+1])
				if !ok1 || !ok2 || hi < lo || hi-lo > 1<<16 {
					break
				}
				if toks[i+2] == "[" {
					j := i + 3
					for c := lo; c <= hi && j < len(toks) && toks[j] != "]"; c, j = c+1, j+1 {
						if dst, ok := hexStr(toks[j]); ok {
							out[c] = dst
						}
					}
					for j < len(toks) && toks[j] != "]" {
						j++
					}
					i = j + 1
					continue
				}
				if dst, ok := hexStr(toks[i+2]); ok && dst != "" {
					rs := []rune(dst)
					last := rs[len(rs)-1]
					for c := lo; c <= hi; c++ {
						rs[len(rs)-1] = last + rune(c-lo)
						out[c] = string(rs)
					}
				}
				i += 3
			}
		}
	}
}

func lexCMap(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '<':
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				return toks
			}
			toks = append(toks, s[i:i+j+1])
			i += j + 1
		case c == '[' || c == ']':
			toks = append(toks, string(c))
			i++
		case c <= ' ':
			i++
		default:
			j := i
			for j < len(s) && s[j] > ' ' && s[j] != '<' && s[j] != '[' && s[j] != ']' {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks
}

func hexTok(t string) (int, bool) {
	if len(t) < 3 || t[0] != '<' || t[len(t)-1] != '>' {
		return 0, false
	}
	h := t[1 : len(t)-1]
	if len(h) > 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

func hexStr(t string) (string, bool) {
	if len(t) < 2 || t[0] != '<' || t[len(t)-1] != '>' {
		return "", false
	}
	h := t[1 : len(t)-1]
	if len(h)%4 == 2 {
		h = "00" + h
	}
	units := make([]uint16, 0, len(h)/4)
	for i := 0; i+4 <= len(h); i += 4 {
		n, err := strconv.ParseUint(h[i:i+4], 16, 16)
		if err != nil {
			return "", false
		}
		units = append(units, uint16(n))
	}
	return string(utf16.Decode(units)), true
}

func parseDifferences(d pdf.Value, out map[int]string) {
	if d.Kind() != pdf.Array {
		return
	}
	code := 0
	for i := 0; i < d.Len(); i++ {
		e := d.Index(i)
		if e.Kind() == pdf.Integer || e.Kind() == pdf.Real {
			code = int(e.Int64())
			continue
		}
		if s := glyphNameToText(e.Name()); s != "" {
			out[code] = s
		}
		code++
	}
}

func glyphNameToText(name string) string {
	if name == "" {
		return ""
	}
	if strings.Contains(name, "_") {
		var b strings.Builder
		for _, part := range strings.Split(name, "_") {
			s := glyphNameToText(part)
			if s == "" {
				return ""
			}
			b.WriteString(s)
		}
		return b.String()
	}
	if strings.HasPrefix(name, "uni") && len(name) == 7 {
		if n, err := strconv.ParseUint(name[3:], 16, 32); err == nil {
			return string(rune(n))
		}
	}
	if strings.HasPrefix(name, "u") && len(name) >= 5 && len(name) <= 7 {
		if n, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return string(rune(n))
		}
	}
	if len(name) == 1 {
		return name
	}
	if s, ok := glyphNames[name]; ok {
		return s
	}
	return ""
}

var glyphNames = map[string]string{
	"space": " ", "exclam": "!", "quotedbl": "\"", "numbersign": "#", "dollar": "$",
	"percent": "%", "ampersand": "&", "quotesingle": "'", "parenleft": "(", "parenright": ")",
	"asterisk": "*", "plus": "+", "comma": ",", "hyphen": "-", "period": ".", "slash": "/",
	"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5",
	"six": "6", "seven": "7", "eight": "8", "nine": "9", "colon": ":", "semicolon": ";",
	"less": "<", "equal": "=", "greater": ">", "question": "?", "at": "@",
	"bracketleft": "[", "backslash": "\\", "bracketright": "]", "underscore": "_",
	"braceleft": "{", "bar": "|", "braceright": "}",
	"adieresis": "ä", "odieresis": "ö", "udieresis": "ü", "Adieresis": "Ä",
	"Odieresis": "Ö", "Udieresis": "Ü", "germandbls": "ß",
	"aacute": "á", "eacute": "é", "iacute": "í", "oacute": "ó", "uacute": "ú", "yacute": "ý",
	"Aacute": "Á", "Eacute": "É", "Iacute": "Í", "Oacute": "Ó", "Uacute": "Ú", "Yacute": "Ý",
	"agrave": "à", "egrave": "è", "igrave": "ì", "ograve": "ò", "ugrave": "ù",
	"acircumflex": "â", "ecircumflex": "ê", "icircumflex": "î", "ocircumflex": "ô", "ucircumflex": "û",
	"atilde": "ã", "ntilde": "ñ", "otilde": "õ", "ccedilla": "ç", "Ccedilla": "Ç",
	"aring": "å", "Aring": "Å", "ae": "æ", "AE": "Æ", "oslash": "ø", "Oslash": "Ø",
	"eth": "ð", "Eth": "Ð", "thorn": "þ", "Thorn": "Þ",
	"fi": "fi", "fl": "fl", "ff": "ff", "ffi": "ffi", "ffl": "ffl",
	"quoteleft": "‘", "quoteright": "’", "quotedblleft": "“", "quotedblright": "”",
	"quotesinglbase": "‚", "quotedblbase": "„", "guillemotleft": "«", "guillemotright": "»",
	"guilsinglleft": "‹", "guilsinglright": "›",
	"endash": "–", "emdash": "—", "bullet": "•", "dagger": "†", "daggerdbl": "‡",
	"ellipsis": "…", "periodcentered": "·", "section": "§", "paragraph": "¶",
	"copyright": "©", "registered": "®", "trademark": "™", "degree": "°",
	"onequarter": "¼", "onehalf": "½", "threequarters": "¾",
	"exclamdown": "¡", "questiondown": "¿", "sterling": "£", "Euro": "€", "cent": "¢",
	"dotlessi": "ı", "oe": "œ", "OE": "Œ", "scaron": "š", "Scaron": "Š",
	"zcaron": "ž", "Zcaron": "Ž", "ydieresis": "ÿ", "Ydieresis": "Ÿ",
	"multiply": "×", "divide": "÷", "minus": "−", "plusminus": "±",
	"nbspace": "\u00a0", "softhyphen": "\u00ad",
}
