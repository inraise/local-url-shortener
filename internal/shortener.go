package internal

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound   = errors.New("short code not found")
	ErrExpired    = errors.New("short link expired")
	ErrInvalidURL = errors.New("invalid url")
	ErrCodeExists = errors.New("code already exists")
)

type Link struct {
	Code        string
	OriginalURL string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

type Store interface {
	Save(ctx context.Context, link Link) error
	GetByCode(ctx context.Context, code string) (Link, error)
}

type MemoryStore struct {
	mu    sync.RWMutex
	links map[string]Link
}

func (m *MemoryStore) GetByCode(_ context.Context, code string) (Link, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	link, exists := m.links[code]
	if !exists {
		return Link{}, ErrNotFound
	}

	return link, nil
}

type CodeGenerator func(n int) (string, error)

type Service struct {
	store   Store
	codeGen CodeGenerator
	now     func() time.Time
}

func NewService(store Store, codeGen CodeGenerator) *Service {
	if codeGen == nil {
		codeGen = randomCode
	}

	return &Service{
		store:   store,
		codeGen: codeGen,
		now:     time.Now,
	}
}

func (s *Service) Shorten(ctx context.Context, rawURL string, ttl time.Duration) (Link, error) {
	normalized, err := validateURL(rawURL)
	if err != nil {
		return Link{}, err
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := s.now().Add(ttl)
		expiresAt = &t
	}

	for attempts := 0; attempts < 10; attempts++ {
		code, err := s.codeGen(8)
		if err != nil {
			return Link{}, err
		}

		link := Link{
			Code:        code,
			OriginalURL: normalized,
			CreatedAt:   s.now(),
			ExpiresAt:   expiresAt,
		}

		err = s.store.Save(ctx, link)
		if err == nil {
			return link, nil
		}
		if errors.Is(err, ErrCodeExists) {
			continue
		}

		return Link{}, err
	}

	return Link{}, fmt.Errorf("could not generate unique code after multiple attempts")
}

func (s *Service) Resolve(ctx context.Context, code string) (Link, error) {
	link, err := s.store.GetByCode(ctx, code)
	if err != nil {
		return Link{}, ErrNotFound
	}

	if link.ExpiresAt != nil && s.now().After(*link.ExpiresAt) {
		return Link{}, ErrExpired
	}

	return link, nil
}

func validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidURL
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", ErrInvalidURL
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidURL
	}
	if parsed.Host == "" {
		return "", ErrInvalidURL
	}

	return parsed.String(), nil
}

func randomCode(n int) (string, error) {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	if n <= 0 {
		return "", errors.New("code length must be positive")
	}

	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}

	return string(out), nil
}

type Handler struct {
	svc     *Service
	baseURL string
}

func NewHandler(svc *Service, baseURL string) *Handler {
	return &Handler{
		svc:     svc,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		h.handleHealth(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/shorten":
		h.handleShorten(w, r)
	case r.Method == http.MethodGet:
		h.handleRedirect(w, r)
	default:
		http.NotFound(w, r)
	}
}

type shortenRequest struct {
	URL        string `json:"url"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type shortenResponse struct {
	Code        string `json:"code"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleShorten(w http.ResponseWriter, r *http.Request) {
	var req shortenRequest
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	if req.TTLSeconds < 0 {
		http.Error(w, "ttl_seconds must be >= 0", http.StatusBadRequest)
		return
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second

	link, err := h.svc.Shorten(r.Context(), req.URL, ttl)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidURL):
			http.Error(w, "invalid url", http.StatusBadRequest)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	resp := shortenResponse{
		Code:        link.Code,
		ShortURL:    h.baseURL + "/" + link.Code,
		OriginalURL: link.OriginalURL,
		CreatedAt:   link.CreatedAt.Format(time.RFC3339),
	}

	if link.ExpiresAt != nil {
		resp.ExpiresAt = link.ExpiresAt.Format(time.RFC3339)
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleRedirect(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	if path == "" || strings.Contains(path, "/") || strings.HasPrefix(path, "api/") || path == "healthz" {
		http.NotFound(w, r)
		return
	}

	link, err := h.svc.Resolve(r.Context(), path)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, ErrExpired):
			http.Error(w, "link expired", http.StatusGone)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, link.OriginalURL, http.StatusFound)
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
