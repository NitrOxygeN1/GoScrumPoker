package repository

import (
	"context"

	"GoScrumPoker/internal/domain"
)

// VoteRepository performs atomic read–modify–write cycles on the room aggregate
// for vote-related fields (delegates to the same storage as RoomRepository).
type VoteRepository interface {
	Apply(ctx context.Context, roomID string, fn func(*domain.Room) error) error
}
