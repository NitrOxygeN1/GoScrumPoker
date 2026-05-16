package auth

import (
	"sync"
	"time"
)

// ProfileStore keeps Google user profiles in memory (replace with a DB in production).
type ProfileStore struct {
	mu   sync.RWMutex
	data map[string]Profile // key: Google user id
}

// NewProfileStore creates an empty profile index.
func NewProfileStore() *ProfileStore {
	return &ProfileStore{data: make(map[string]Profile)}
}

// Upsert saves or replaces a profile keyed by Google user id.
func (s *ProfileStore) Upsert(p Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Updated = time.Now().Unix()
	s.data[p.GoogleID] = p
}

// Get returns a profile by Google user id.
func (s *ProfileStore) Get(googleID string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[googleID]
	return p, ok
}
