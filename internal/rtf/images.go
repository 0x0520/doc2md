package rtf

import (
	"encoding/hex"
	"strings"

	"github.com/0x0520/doc2md/internal/docimg"
)

// maxPictureBytes caps the total picture data extracted from one document.
const maxPictureBytes = 128 << 20

// blipFormats maps picture control words to extensions. Only PNG and JPEG are
// extracted; metafile and bitmap pictures are skipped.
var blipFormats = map[string]string{
	"pngblip":  "png",
	"jpegblip": "jpg",
}

// picture decodes the \pict group starting at from, writes its marker into the
// text and returns the index of the group's closing brace, or -1 if it is unclosed.
func (p *parser) picture(s string, from int) int {
	end, body := pictGroup(s, from)
	if end < 0 {
		return -1
	}
	ext, payload := blipPayload(body)
	if ext == "" || p.total >= maxPictureBytes {
		return end
	}
	b, err := hex.DecodeString(payload)
	if err != nil || len(b) == 0 {
		return end
	}
	p.total += len(b)
	p.seq++
	p.images = append(p.images, docimg.Image{Seq: p.seq, Bytes: b, Ext: ext})
	p.write(" " + docimg.MarkerFor(p.seq) + " ")
	return end
}

// pictGroup returns the index of the brace closing the current group and its contents.
func pictGroup(s string, from int) (end int, body string) {
	depth := 1
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i, s[from:i]
			}
		}
	}
	return -1, ""
}

// blipPayload finds the PNG/JPEG control word in a \pict body and returns the
// extension and the hex-encoded image data that follows the width, height and crop
// control words and nested groups.
func blipPayload(body string) (ext, payload string) {
	for word, e := range blipFormats {
		i := strings.Index(body, `\`+word)
		if i < 0 {
			continue
		}
		ext = e
		body = body[i+len(word)+1:]
		break
	}
	if ext == "" {
		return "", ""
	}

	i := 0
scan:
	for i < len(body) {
		switch c := body[i]; {
		case c == ' ' || c == '\r' || c == '\n' || c == '\t':
			i++
		case c == '{':
			depth := 1
			for i++; i < len(body) && depth > 0; i++ {
				switch body[i] {
				case '\\':
					i++
				case '{':
					depth++
				case '}':
					depth--
				}
			}
		case c == '\\':
			i++
			for i < len(body) && isAlpha(body[i]) {
				i++
			}
			if i < len(body) && body[i] == '-' {
				i++
			}
			for i < len(body) && isDigit(body[i]) {
				i++
			}
			if i < len(body) && body[i] == ' ' {
				i++
			}
		default:
			break scan
		}
	}
	body = body[i:]

	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case isHexDigit(c):
			b.WriteByte(c)
		case c == ' ' || c == '\r' || c == '\n' || c == '\t':
		default:
			i = len(body)
		}
	}
	payload = b.String()
	if len(payload)%2 == 1 {
		payload = payload[:len(payload)-1]
	}
	if payload == "" {
		return "", ""
	}
	return ext, payload
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
