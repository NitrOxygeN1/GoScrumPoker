package repository

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"GoScrumPoker/internal/domain"
)

// Memory is an in-process implementation of RoomRepository and VoteRepository.
type Memory struct {
	mu    sync.Mutex
	rooms map[string]*domain.Room
}

var _ RoomRepository = (*Memory)(nil)
var _ VoteRepository = (*Memory)(nil)

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{rooms: make(map[string]*domain.Room)}
}

// Close is a no-op for memory.
func (m *Memory) Close() error {
	return nil
}

// CreateRoom implements RoomRepository.
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

// Exists implements RoomRepository.
func (m *Memory) Exists(ctx context.Context, id string) (bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rooms[id]
	return ok, nil
}

// Snapshot implements RoomRepository.
func (m *Memory) Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[id]
	if !ok {
		return domain.RoomState{}, false, nil
	}
	return domain.BuildRoomState(r), true, nil
}

// Join implements RoomRepository.
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

// Leave implements RoomRepository.
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

// Apply implements VoteRepository.
func (m *Memory) Apply(ctx context.Context, roomID string, fn func(*domain.Room) error) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return ErrRoomNotFound
	}
	return fn(r)
}
