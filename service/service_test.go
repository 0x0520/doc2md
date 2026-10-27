package service

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, filename string, data []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if filename != "" {
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/convert", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func do(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestConvertAnswersWithMarkdown(t *testing.T) {
	h := Handler(Options{})
	csv := []byte("Item;Count\nWidget;3\n")
	rec := do(h, post(t, "inventory.csv", csv, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got wireResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("answer is not JSON: %v", err)
	}
	if got.Name != "inventory.csv" || got.Format != "csv" || got.Bytes != len(csv) {
		t.Errorf("name/format/bytes = %q/%q/%d, want inventory.csv/csv/%d", got.Name, got.Format, got.Bytes, len(csv))
	}
	if !strings.Contains(got.Markdown, "Item") || !strings.Contains(got.Markdown, "Widget") {
		t.Errorf("the Markdown lost the table:\n%s", got.Markdown)
	}
}

func TestConvertRefusesAnUnsupportedFormatWith415(t *testing.T) {
	h := Handler(Options{})
	ole := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	rec := do(h, post(t, "legacy_2011.doc", ole, nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 (body: %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == nil {
		t.Fatalf("415 should name its reason, got %s", rec.Body.String())
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, ".doc") || !strings.Contains(msg, ".docx") {
		t.Errorf("the refusal does not say what to do: %s", msg)
	}
}

func TestConvertRefusesADocumentOverTheLimit(t *testing.T) {
	h := Handler(Options{MaxBytes: 1024})
	rec := do(h, post(t, "large.txt", bytes.Repeat([]byte("x"), 4096), nil))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestConvertWithoutAFileIsABadRequest(t *testing.T) {
	h := Handler(Options{})
	rec := do(h, post(t, "", nil, map[string]string{"verbatim": "1"}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestVerbatimIsPassedOn(t *testing.T) {
	h := Handler(Options{})
	rec := do(h, post(t, "note.md", []byte("# Title\n"), map[string]string{"verbatim": "1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got wireResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("answer is not JSON: %v", err)
	}
	if !got.Verbatim {
		t.Errorf("verbatim did not survive the request")
	}
}

func TestConfigReportsLimitAndFormats(t *testing.T) {
	rec := do(Handler(Options{}), httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var cfg struct {
		MaxBytes int64    `json:"maxBytes"`
		Formats  []string `json:"formats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	if cfg.MaxBytes != DefaultMaxBytes || len(cfg.Formats) == 0 {
		t.Errorf("config is incomplete: %s", rec.Body.String())
	}
}

func TestThePanelIsServed(t *testing.T) {
	rec := do(Handler(Options{}), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<title>doc2md</title>") {
		t.Errorf("that does not look like the panel: %.80s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestAnUnknownAPIPathIsNotSwallowedByThePanel(t *testing.T) {
	rec := do(Handler(Options{}), httptest.NewRequest(http.MethodGet, "/api/convrt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}

func TestNoUIServesTheAPIOnly(t *testing.T) {
	h := Handler(Options{NoUI: true})
	if rec := do(h, httptest.NewRequest(http.MethodGet, "/", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("with NoUI, / = %d, want 404", rec.Code)
	}
	if rec := do(h, httptest.NewRequest(http.MethodGet, "/api/health", nil)); rec.Code != http.StatusOK {
		t.Errorf("health = %d, want 200", rec.Code)
	}
}

func TestThePanelsAssetsAreServed(t *testing.T) {
	h := Handler(Options{})
	for _, path := range []string{"/app.css", "/app.js"} {
		rec := do(h, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
			continue
		}
		if _, err := io.ReadAll(rec.Body); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
