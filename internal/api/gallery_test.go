package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

func TestGalleryAndDiaryImages(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	auth := authMiddlewareFor(user)
	RegisterMediaRoutes(e, s, auth)
	RegisterMediaLibraryRoutes(e, s, auth, medialib.New(s, nil))
	RegisterDiaryRoutes(e, s, auth, nil)
	jsonHeader := map[string]string{"Content-Type": "application/json"}

	uploadTestMedia(t, s, user.ID, "m1", time.Now())
	uploadTestMedia(t, s, user.ID, "m2", time.Now())
	content := `<p><img src="/api/v1/files/media/m1/m1.png"><img src="https://img.example.com/ext.png"></p>`
	body, _ := json.Marshal(map[string]string{"date": "2024-04-05", "content": content})
	rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/upsert", bytes.NewReader(body), jsonHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert = %d %s", rec.Code, rec.Body.String())
	}
	var saved struct {
		Images []store.DiaryImage `json:"images"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || len(saved.Images) != 2 || !saved.Images[0].Managed || saved.Images[0].MediaID != "m1" || saved.Images[1].Managed {
		t.Fatalf("upsert images = %+v %v", saved.Images, err)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-04-05", nil, nil)
	if !strings.Contains(rec.Body.String(), `"url":"https://img.example.com/ext.png"`) {
		t.Fatalf("by-date = %s", rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2020-01-01", nil, nil)
	if !strings.Contains(rec.Body.String(), `"images":[]`) {
		t.Fatalf("missing entry = %s", rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/media/gallery?perPage=500", nil, nil)
	var gallery struct {
		TotalItems int                 `json:"totalItems"`
		PerPage    int                 `json:"perPage"`
		Items      []store.GalleryItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &gallery); err != nil || rec.Code != http.StatusOK || gallery.TotalItems != 3 || gallery.PerPage != 200 {
		t.Fatalf("gallery = %d %s", rec.Code, rec.Body.String())
	}
	external := 0
	for _, item := range gallery.Items {
		if item.Kind == store.GalleryKindExternal {
			external++
			if item.Managed || item.URL != "https://img.example.com/ext.png" {
				t.Fatalf("external item = %+v", item)
			}
		}
	}
	if external != 1 {
		t.Fatalf("external items = %d", external)
	}

	// Several images to the trash at once; external images have no media
	// record, so they cannot be trashed.
	result := postMediaIDs(t, e, "/api/v1/media/trash", "m1", "m2", "https://img.example.com/ext.png")
	if len(result.Done) != 2 || result.Failed["https://img.example.com/ext.png"] != "not found" {
		t.Fatalf("batch trash = %+v", result)
	}
	if rec := performRequest(t, e, http.MethodGet, "/api/v1/media/gallery", nil, nil); !strings.Contains(rec.Body.String(), `"totalItems":1`) {
		t.Fatalf("gallery after trash = %s", rec.Body.String())
	}
	if rec := performRequest(t, e, http.MethodPost, "/api/v1/media/trash", strings.NewReader(`{"ids":[]}`), jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty batch = %d", rec.Code)
	}

	_ = s.Close()
	if rec := performRequest(t, e, http.MethodGet, "/api/v1/media/gallery", nil, nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("gallery on a closed store = %d", rec.Code)
	}
}
