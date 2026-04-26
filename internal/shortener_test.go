package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServiceShortenAndResolve(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, func(n int) (string, error) {
		return "abc12345", nil
	})
	svc.now = func() time.Time {
		return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	}

	link, err := svc.Shorten(t.Context(), "https://example.com", 0)
	if err != nil {
		t.Fatalf("shorten failed: %v", err)
	}

	if link.Code != "abc12345" {
		t.Fatalf("unexpected code: %s", link.Code)
	}

	got, err := svc.Resolve(t.Context(), "abc12345")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if got.OriginalURL != "https://example.com" {
		t.Fatalf("unexpected url: %s", got.OriginalURL)
	}
}

func TestHandlerShortenAndRedirect(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, func(n int) (string, error) {
		return "abc12345", nil
	})
	h := NewHandler(svc, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"url":"https://example.com"}`))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d", http.StatusCreated, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), `"code":"abc12345"`) {
		t.Fatalf("response does not contain code: %s", rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/abc12345", nil)
	rec2 := httptest.NewRecorder()

	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec2.Code)
	}

	if loc := rec2.Header().Get("Location"); loc != "https://example.com" {
		t.Fatalf("unexpected location: %s", loc)
	}
}

func TestHandlerRejectsBadURL(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, func(n int) (string, error) {
		return "abc12345", nil
	})
	h := NewHandler(svc, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"url":"not-a-url"}`))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}
