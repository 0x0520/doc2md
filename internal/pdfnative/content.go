package pdfnative

import (
	"strings"

	"github.com/ledongthuc/pdf"
)

// maxFormDepth bounds the nesting of form XObjects; deeper chains are skipped.
const maxFormDepth = 12

// extractTokens interprets a page's content stream and returns one token per
// character code, positioned in page space, plus the image draws. It tracks the
// text state itself so that font widths, ToUnicode maps and embedded-font
// ligatures can be applied. Form XObjects are interpreted in place.
func extractTokens(p pdf.Page) ([]pdf.Text, []imgPlace) {
	contents := p.V.Key("Contents")
	if contents.Kind() == pdf.Null {
		return nil, nil
	}
	in := &interpreter{fonts: map[string]map[string]*fontInfo{}, active: map[string]bool{}}
	in.run(contents, p.Resources(), "", gstate{ctm: identM, tm: identM, tlm: identM, th: 1}, 0)
	return in.out, in.places
}

// fontInfo is what a Tf operator needs from a font resource, built once per resource dictionary.
type fontInfo struct {
	baseFont string
	enc      pdf.TextEncoding
	metrics  *fontMetrics
	decode   map[int]string
}

// interpreter collects the tokens and image draws of one page across its form XObjects.
// fonts caches fontInfo per resource scope ("" is the page) and font name; active
// holds the forms being interpreted, so a form that draws itself is not entered again.
type interpreter struct {
	out    []pdf.Text
	places []imgPlace
	fonts  map[string]map[string]*fontInfo
	active map[string]bool
}

func (in *interpreter) font(res pdf.Value, scope, name string) *fontInfo {
	cache := in.fonts[scope]
	if cache == nil {
		cache = map[string]*fontInfo{}
		in.fonts[scope] = cache
	}
	if fi, ok := cache[name]; ok {
		return fi
	}
	font := pdf.Font{V: res.Key("Font").Key(name)}
	fi := &fontInfo{
		baseFont: styledName(stripSubsetPrefix(font.BaseFont()), font),
		enc:      font.Encoder(),
		metrics:  buildMetrics(font),
		decode:   buildDecoder(font),
	}
	cache[name] = fi
	return fi
}

// form interprets a form XObject (PDF 32000-1 §8.10) with its /Matrix applied to the
// current CTM. The state is restored afterwards, as if the call were wrapped in q/Q.
func (in *interpreter) form(xo, parentRes pdf.Value, parentScope string, g gstate, depth int) {
	// A stream's printed form ends in its file offset, so it identifies the object.
	key := xo.String()
	if depth >= maxFormDepth || in.active[key] {
		return
	}
	in.active[key] = true
	defer delete(in.active, key)
	// A broken form loses only its own text, not the rest of the page.
	defer func() { _ = recover() }()

	res, scope := xo.Key("Resources"), key
	// Forms without their own resources use the caller's (allowed for PDF 1.1 files).
	if res.IsNull() {
		res, scope = parentRes, parentScope
	}
	if m := xo.Key("Matrix"); m.Kind() == pdf.Array && m.Len() == 6 {
		args := make([]pdf.Value, 6)
		for i := range args {
			args[i] = m.Index(i)
		}
		g.ctm = matrixFromArgs(args).mul(g.ctm)
	}
	in.run(xo, res, scope, g, depth+1)
}

func (in *interpreter) run(contents, res pdf.Value, scope string, g gstate, depth int) {
	var stack []gstate

	// showText emits a token per character code and advances the text matrix.
	showText := func(raw string) {
		if g.font == nil {
			return
		}
		fi := g.font
		step := 1
		// Type0 fonts are read as two-byte codes.
		if fi.metrics.type0 {
			step = 2
		}
		for k := 0; k+step <= len(raw); k += step {
			code := int(raw[k])
			if step == 2 {
				code = int(raw[k])<<8 | int(raw[k+1])
			}
			seg := raw[k : k+step]
			txt := seg
			if s, ok := fi.decode[code]; ok && s != "" {
				txt = s
			} else if fi.enc != nil {
				txt = fi.enc.Decode(seg)
			}
			txt = expandLigatures(txt)
			w0 := fi.metrics.width(code, txt)

			// Text rendering matrix (PDF 32000-1 §9.4.4): gives device-space position and size.
			trm := textScale(g.tfs, g.th, g.trise).mul(g.tm).mul(g.ctm)
			in.out = append(in.out, pdf.Text{
				Font:     fi.baseFont,
				FontSize: trm[0][0],
				X:        trm[2][0],
				Y:        trm[2][1],
				W:        w0 / 1000 * trm[0][0],
				S:        txt,
			})

			tx := w0 / 1000 * g.tfs
			// Word spacing applies only to the single-byte code 32.
			if step == 1 && code == 32 {
				tx += g.tw
			}
			// Horizontal advance per PDF 32000-1 §9.4.4: ((w0·Tfs + Tc + Tw) · Th).
			tx = (tx + g.tc) * g.th
			g.tm = translateM(tx, 0).mul(g.tm)
		}
	}

	pdf.Interpret(contents, func(stk *pdf.Stack, op string) {
		n := stk.Len()
		args := make([]pdf.Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		case "cm":
			if len(args) == 6 {
				g.ctm = matrixFromArgs(args).mul(g.ctm)
			}
		case "q":
			stack = append(stack, g)
		case "Q":
			if len(stack) > 0 {
				g = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "BT":
			g.tm, g.tlm = identM, identM
		case "Td":
			if len(args) == 2 {
				g.tlm = translateM(args[0].Float64(), args[1].Float64()).mul(g.tlm)
				g.tm = g.tlm
			}
		case "TD":
			if len(args) == 2 {
				g.tl = -args[1].Float64()
				g.tlm = translateM(args[0].Float64(), args[1].Float64()).mul(g.tlm)
				g.tm = g.tlm
			}
		case "Tm":
			if len(args) == 6 {
				g.tm = matrixFromArgs(args)
				g.tlm = g.tm
			}
		case "T*":
			g.tlm = translateM(0, -g.tl).mul(g.tlm)
			g.tm = g.tlm
		case "Tc":
			if len(args) == 1 {
				g.tc = args[0].Float64()
			}
		case "Tw":
			if len(args) == 1 {
				g.tw = args[0].Float64()
			}
		case "Tz":
			if len(args) == 1 {
				g.th = args[0].Float64() / 100
			}
		case "TL":
			if len(args) == 1 {
				g.tl = args[0].Float64()
			}
		case "Ts":
			if len(args) == 1 {
				g.trise = args[0].Float64()
			}
		case "Tf":
			if len(args) == 2 {
				g.font = in.font(res, scope, args[0].Name())
				g.tfs = args[1].Float64()
			}
		case "\"":
			if len(args) == 3 {
				g.tw = args[0].Float64()
				g.tc = args[1].Float64()
				g.tlm = translateM(0, -g.tl).mul(g.tlm)
				g.tm = g.tlm
				showText(args[2].RawString())
			}
		case "'":
			if len(args) == 1 {
				g.tlm = translateM(0, -g.tl).mul(g.tlm)
				g.tm = g.tlm
				showText(args[0].RawString())
			}
		case "Tj":
			if len(args) == 1 {
				showText(args[0].RawString())
			}
		case "TJ":
			if len(args) == 1 {
				v := args[0]
				for i := 0; i < v.Len(); i++ {
					x := v.Index(i)
					if x.Kind() == pdf.String {
						showText(x.RawString())
						continue
					}
					// A TJ number is thousandths of text space, subtracted from the advance.
					tx := -x.Float64() / 1000 * g.tfs * g.th
					g.tm = translateM(tx, 0).mul(g.tm)
				}
			}
		case "Do":
			if len(args) != 1 {
				break
			}
			xo := res.Key("XObject").Key(args[0].Name())
			if xo.Key("Subtype").Name() == "Form" {
				in.form(xo, res, scope, g, depth)
				break
			}
			in.places = append(in.places, imgPlace{
				name:   args[0].Name(),
				topY:   g.ctm[2][1] + absF(g.ctm[1][1]),
				height: absF(g.ctm[1][1]),
			})
		}
	})
}

// imgPlace is an image draw: the XObject name, its top edge and height in page space.
type imgPlace struct {
	name   string
	topY   float64
	height float64
}

// gstate is the part of the graphics and text state that affects glyph placement:
// font size tfs, character spacing tc, word spacing tw, horizontal scale th
// (1 = 100%), leading tl.
type gstate struct {
	ctm, tm, tlm        matrix3
	font                *fontInfo
	tfs, tc, tw, th, tl float64
	trise               float64
}

// styledName adds the ",Bold" / ",Italic" style suffix of PDF font names (PDF 32000-1
// §9.6.2.1) when the font descriptor declares a style the name does not show, so
// that the name-based checks see it. Bold is a weight of 600 or more or the
// ForceBold flag; italic is the Italic flag or a slanted italic angle.
func styledName(name string, font pdf.Font) string {
	fd := font.V.Key("FontDescriptor")
	if font.V.Key("Subtype").Name() == "Type0" {
		fd = font.V.Key("DescendantFonts").Index(0).Key("FontDescriptor")
	}
	if fd.IsNull() {
		return name
	}
	flags := fd.Key("Flags").Int64()
	bold := !isBoldFont(name) && (fd.Key("FontWeight").Float64() >= 600 || flags&(1<<18) != 0)
	italic := !isItalicFont(name) && (flags&(1<<6) != 0 || fd.Key("ItalicAngle").Float64() != 0)
	switch {
	case bold && italic:
		return name + ",BoldItalic"
	case bold:
		return name + ",Bold"
	case italic:
		return name + ",Italic"
	}
	return name
}

// stripSubsetPrefix removes the "ABCDEF+" subset tag from a font name.
func stripSubsetPrefix(name string) string {
	if i := strings.Index(name, "+"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// ligatures expands presentation-form ligature code points to plain letters.
var ligatures = strings.NewReplacer(
	"ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl",
	"ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "ft", "ﬆ", "st",
)

func expandLigatures(s string) string {
	// Pure ASCII, the common case, skips the replacer.
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return ligatures.Replace(s)
		}
	}
	return s
}

// matrix3 is a PDF transformation matrix in row-vector form; a.mul(b) applies a first, then b.
type matrix3 [3][3]float64

var identM = matrix3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func (a matrix3) mul(b matrix3) matrix3 {
	var r matrix3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return r
}

func translateM(tx, ty float64) matrix3 {
	return matrix3{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
}

// textScale is the font size, horizontal scaling and rise matrix of PDF 32000-1 §9.4.4.
func textScale(tfs, th, trise float64) matrix3 {
	return matrix3{{tfs * th, 0, 0}, {0, tfs, 0}, {0, trise, 1}}
}

func matrixFromArgs(args []pdf.Value) matrix3 {
	m := matrix3{}
	for i := 0; i < 6; i++ {
		m[i/2][i%2] = args[i].Float64()
	}
	m[2][2] = 1
	return m
}
