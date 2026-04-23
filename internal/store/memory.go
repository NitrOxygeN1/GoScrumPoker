package store

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"

	"GoScrumPoker/internal/domain"
)

// Memory is a thread-safe in-memory room index (single instance only).
type Memory struct {
	mu    sync.RWMutex
	rooms map[string]*domain.Room
}

// NewMemory creates an empty store.
func NewMemory() *Memory {
	return &Memory{rooms: make(map[string]*domain.Room)}
}

// Close releases resources (no-op for memory).
func (m *Memory) Close() error {
	return nil
}

// CreateRoom allocates a new room and returns it.
func (m *Memory) CreateRoom(ctx context.Context) (*domain.Room, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.NewString()
	for m.rooms[id] != nil {
		id = uuid.NewString()
	}
	r := domain.NewRoom(id)
	m.rooms[id] = r
	return r, nil
}

// Exists reports whether a room identifier is known.
func (m *Memory) Exists(ctx context.Context, id string) (bool, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.rooms[id]
	return ok, nil
}

// Snapshot returns a client-safe view.
func (m *Memory) Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[id]
	if !ok {
		return domain.RoomState{}, false, nil
	}
	return domain.BuildRoomState(r), true, nil
}

// Join adds or updates a user in the room.
func (m *Memory) Join(ctx context.Context, roomID string, user domain.User) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	r.Users[user.ID] = user
	return nil
}

// Vote records or updates a user's vote while votes are hidden.
func (m *Memory) Vote(ctx context.Context, roomID, userID, value string) error {
	_ = ctx
	value = strings.TrimSpace(value)
	if value == "" {
		return ErrInvalidVote
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	if _, ok := r.Users[userID]; !ok {
		return ErrUserNotInRoom
	}
	if r.Revealed {
		return ErrVotesRevealed
	}
	r.Votes[userID] = value
	return nil
}

// Reveal exposes all recorded votes.
func (m *Memory) Reveal(ctx context.Context, roomID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	r.Revealed = true
	return nil
}

// Reset clears votes and hides them again.
func (m *Memory) Reset(ctx context.Context, roomID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	r.Revealed = false
	r.Votes = make(map[string]string)
	return nil
}

// Leave removes a user and their vote from a room.
func (m *Memory) Leave(ctx context.Context, roomID, userID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	delete(r.Users, userID)
	delete(r.Votes, userID)
	return nil
}
