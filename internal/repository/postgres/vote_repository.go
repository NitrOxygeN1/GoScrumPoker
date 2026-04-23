package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
)

// PostgresVoteRepository implements repository.VoteRepository using PostgreSQL.
type PostgresVoteRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresVoteRepository constructs a Postgres-backed vote repository.
func NewPostgresVoteRepository(pool *pgxpool.Pool) *PostgresVoteRepository {
	return &PostgresVoteRepository{pool: pool}
}

// Apply runs fn on the current room aggregate inside a transaction.
func (v *PostgresVoteRepository) Apply(ctx context.Context, roomID string, fn func(*domain.Room) error) error {
	rid, err := parseRoomID(roomID)
	if err != nil {
		return fmt.Errorf("invalid room id: %w", err)
	}

	tx, err := v.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM rooms WHERE id = $1 FOR UPDATE`, rid).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrRoomNotFound
		}
		return fmt.Errorf("lock room: %w", err)
	}

	room, err := loadRoom(ctx, tx, rid)
	if err != nil {
		return err
	}

	if err := fn(room); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE rooms SET revealed = $1 WHERE id = $2`, room.Revealed, rid); err != nil {
		return fmt.Errorf("update revealed: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE room_id = $1`, rid); err != nil {
		return fmt.Errorf("clear votes: %w", err)
	}
	for uid, val := range room.Votes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO votes (room_id, user_id, value) VALUES ($1, $2, $3)`,
			rid, uid, val,
		); err != nil {
			return fmt.Errorf("insert vote: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}
