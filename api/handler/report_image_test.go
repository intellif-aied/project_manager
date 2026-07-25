package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type reportImageMemoryStore struct {
	name        string
	contentType string
	content     []byte
}

func (s *reportImageMemoryStore) Upload(_ context.Context, name string, reader io.Reader, _ int64, contentType string) error {
	s.name, s.contentType = name, contentType
	s.content, _ = io.ReadAll(reader)
	return nil
}
func (s *reportImageMemoryStore) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.content)), nil
}

func reportImageUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/report-images", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestReportImageUploadStoresPNGAndReturnsMarkdownURL(t *testing.T) {
	store := &reportImageMemoryStore{}
	recorder := httptest.NewRecorder()
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, 32)...)
	NewReportImageHandler(store).Upload(recorder, reportImageUploadRequest(t, "shot.png", png))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.contentType != "image/png" || !strings.HasPrefix(store.name, "report-images/") {
		t.Fatalf("stored name=%q content-type=%q", store.name, store.contentType)
	}
	if !strings.Contains(recorder.Body.String(), "/api/v1/report-images/") {
		t.Fatalf("response=%s", recorder.Body.String())
	}
}

func TestReportImageUploadRejectsUnsupportedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewReportImageHandler(&reportImageMemoryStore{}).Upload(recorder, reportImageUploadRequest(t, "x.svg", []byte("<svg></svg>")))
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReportImageUploadRejectsFilesOverFiveMegabytes(t *testing.T) {
	recorder := httptest.NewRecorder()
	largePNG := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, int(maxReportImageSize))...)
	NewReportImageHandler(&reportImageMemoryStore{}).Upload(recorder, reportImageUploadRequest(t, "large.png", largePNG))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "5MB") {
		t.Fatalf("response=%s", recorder.Body.String())
	}
}
