package repository

import (
	"context"

	"GoScrumPoker/internal/domain"
)

// RoomRepository persists room identity, participants, and lifecycle.
// Implementations must be safe for concurrent use across instances where applicable.
type RoomRepository interface {
	CreateRoom(ctx context.Context) (*domain.Room, error)
	Exists(ctx context.Context, id string) (bool, error)
	Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error)
	Join(ctx context.Context, roomID string, user domain.User) error
	Leave(ctx context.Context, roomID, userID string) error
	Close() error
}
