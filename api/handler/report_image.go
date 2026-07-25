package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxReportImageSize int64 = 5 << 20

var reportImageNamePattern = regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|webp)$`)

type reportImageStore interface {
	Upload(context.Context, string, io.Reader, int64, string) error
	Download(context.Context, string) (io.ReadCloser, error)
}

type ReportImageHandler struct {
	store reportImageStore
}

func NewReportImageHandler(store reportImageStore) *ReportImageHandler {
	return &ReportImageHandler{store: store}
}

func (h *ReportImageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "图片存储暂不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReportImageSize+(1<<20))
	file, header, err := r.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "图片不能超过 5MB"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择要上传的图片"})
		return
	}
	defer file.Close()
	if header.Size <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "图片内容为空"})
		return
	}
	if header.Size > maxReportImageSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "图片不能超过 5MB"})
		return
	}

	head := make([]byte, 512)
	n, readErr := io.ReadFull(file, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法读取图片"})
		return
	}
	contentType := http.DetectContentType(head[:n])
	extension := map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpg",
		"image/webp": "webp",
	}[contentType]
	if extension == "" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "仅支持 PNG、JPEG、WebP 图片"})
		return
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成图片标识失败"})
		return
	}
	name := hex.EncodeToString(token) + "." + extension
	objectName := "report-images/" + name
	reader := io.MultiReader(bytes.NewReader(head[:n]), file)
	if err := h.store.Upload(r.Context(), objectName, reader, header.Size, contentType); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "图片上传失败，请稍后重试"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":          "/api/v1/report-images/" + name,
		"name":         header.Filename,
		"size":         header.Size,
		"content_type": contentType,
	})
}

func (h *ReportImageHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "name")
	if !reportImageNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	reader, err := h.store.Download(r.Context(), "report-images/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	switch {
	case strings.HasSuffix(name, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(name, ".jpg"):
		w.Header().Set("Content-Type", "image/jpeg")
	case strings.HasSuffix(name, ".webp"):
		w.Header().Set("Content-Type", "image/webp")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = io.Copy(w, reader)
}
