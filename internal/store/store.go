package store

import (
	"context"

	"GoScrumPoker/internal/domain"
)

// RoomStore persists Scrum Poker rooms. Implementations must be safe for concurrent use
// across multiple application instances (e.g. Redis); in-process memory is for tests/dev.
type RoomStore interface {
	CreateRoom(ctx context.Context) (*domain.Room, error)
	Exists(ctx context.Context, id string) (bool, error)
	Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error)
	Join(ctx context.Context, roomID string, user domain.User) error
	Vote(ctx context.Context, roomID, userID, value string) error
	Reveal(ctx context.Context, roomID string) error
	Reset(ctx context.Context, roomID string) error
	Leave(ctx context.Context, roomID, userID string) error
	Close() error
}
