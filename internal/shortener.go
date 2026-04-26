package internal

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
