package internal

import (
	"context"
	"errors"
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
