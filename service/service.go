package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/0x0520/doc2md"
	"github.com/0x0520/doc2md/service/ui"
)

const DefaultMaxBytes = 32 << 20

type Options struct {
	MaxBytes int64
	NoUI     bool
}

func (o Options) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return DefaultMaxBytes
}

func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"maxBytes": opts.maxBytes(),
			"formats":  doc2md.Extensions(),
		})
	})
	mux.HandleFunc("POST /api/convert", func(w http.ResponseWriter, r *http.Request) {
		convert(w, r, opts.maxBytes())
	})
	if !opts.NoUI {
		mux.Handle("/", withoutAPI(ui.Handler()))
	}
	return mux
}

func withoutAPI(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, fmt.Errorf("%s %s: no such endpoint", r.Method, r.URL.Path))
			return
		}
		files.ServeHTTP(w, r)
	})
}

type wireImage struct {
	Seq        int    `json:"seq"`
	Page       int    `json:"page"`
	Ext        string `json:"ext"`
	DataBase64 string `json:"dataBase64"`
}

type wireResult struct {
	Name     string      `json:"name"`
	Format   string      `json:"format"`
	Bytes    int         `json:"bytes"`
	Verbatim bool        `json:"verbatim"`
	Markdown string      `json:"markdown"`
	Images   []wireImage `json:"images,omitempty"`
	Millis   int64       `json:"ms"`
}

func convert(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, errTooLarge(maxBytes))
			return
		}
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unreadable form: %w", err))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(`missing form field "file"`))
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unreadable document: %w", err))
		return
	}
	if int64(len(data)) > maxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, errTooLarge(maxBytes))
		return
	}

	name := filepath.Base(header.Filename)
	verbatim := isTrue(r.FormValue("verbatim"))

	conv := doc2md.Convert
	if verbatim {
		conv = doc2md.ConvertVerbatim
	}
	started := time.Now()
	md, images, err := conv(name, data)
	if err != nil {
		if errors.Is(err, doc2md.ErrUnsupportedFormat) {
			writeErr(w, http.StatusUnsupportedMediaType, errors.New(refusal(name)+wayOut(name)))
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}

	out := wireResult{
		Name:     name,
		Format:   strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
		Bytes:    len(data),
		Verbatim: verbatim,
		Markdown: md,
		Millis:   time.Since(started).Milliseconds(),
	}
	for _, img := range images {
		out.Images = append(out.Images, wireImage{
			Seq: img.Seq, Page: img.Page, Ext: img.Ext,
			DataBase64: base64.StdEncoding.EncodeToString(img.Bytes),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func errTooLarge(maxBytes int64) error {
	return fmt.Errorf("document exceeds the limit of %d bytes", maxBytes)
}

func refusal(name string) string {
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."); ext != "" {
		return "This format is not supported (." + ext + ")."
	}
	return "This document does not match any supported format."
}

func wayOut(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".doc":
		return " It is a legacy Word 97-2003 file: open it in Word, save it as .docx and upload it again."
	case ".ppt":
		return " It is a legacy PowerPoint 97-2003 file: open it in PowerPoint, save it as .pptx and upload it again."
	case ".msg", ".eml":
		return " An email is a container; upload its attachments one by one."
	case ".pages", ".numbers", ".key":
		return " Apple iWork formats are not supported: export the file from Pages, Numbers or Keynote as PDF or as an Office format."
	default:
		return " Supported: PDF, Word, PowerPoint, Excel, CSV, OpenDocument, EPUB, RTF, HTML, XML, AsciiDoc and Markdown."
	}
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}
