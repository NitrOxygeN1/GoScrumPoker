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

// PostgresRoomRepository implements repository.RoomRepository using PostgreSQL.
type PostgresRoomRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRoomRepository constructs a Postgres-backed room repository.
func NewPostgresRoomRepository(pool *pgxpool.Pool) *PostgresRoomRepository {
	return &PostgresRoomRepository{pool: pool}
}

// Close is a no-op; the shared pool is closed by the caller.
func (r *PostgresRoomRepository) Close() error {
	return nil
}

// CreateRoom implements repository.RoomRepository.
func (r *PostgresRoomRepository) CreateRoom(ctx context.Context) (*domain.Room, error) {
	id := uuid.New()
	_, err := r.pool.Exec(ctx, `INSERT INTO rooms (id) VALUES ($1)`, id)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}
	return domain.NewRoom(id.String()), nil
}

// Exists implements repository.RoomRepository.
func (r *PostgresRoomRepository) Exists(ctx context.Context, id string) (bool, error) {
	rid, err := parseRoomID(id)
	if err != nil {
		return false, fmt.Errorf("invalid room id: %w", err)
	}
	var ok bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rooms WHERE id = $1)`, rid).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}
	return ok, nil
}

// Snapshot implements repository.RoomRepository.
func (r *PostgresRoomRepository) Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error) {
	rid, err := parseRoomID(id)
	if err != nil {
		return domain.RoomState{}, false, fmt.Errorf("invalid room id: %w", err)
	}
	room, err := loadRoom(ctx, r.pool, rid)
	if err != nil {
		if errors.Is(err, repository.ErrRoomNotFound) {
			return domain.RoomState{}, false, nil
		}
		return domain.RoomState{}, false, err
	}
	return domain.BuildRoomState(room), true, nil
}

// Join implements repository.RoomRepository.
func (r *PostgresRoomRepository) Join(ctx context.Context, roomID string, user domain.User) error {
	rid, err := parseRoomID(roomID)
	if err != nil {
		return fmt.Errorf("invalid room id: %w", err)
	}
	var one int
	if err := r.pool.QueryRow(ctx, `SELECT 1 FROM rooms WHERE id = $1`, rid).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrRoomNotFound
		}
		return fmt.Errorf("room lookup: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO room_participants (room_id, user_id, display_name) VALUES ($1, $2, $3)
		 ON CONFLICT (room_id, user_id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		rid, user.ID, user.Name,
	)
	if err != nil {
		return fmt.Errorf("join participant: %w", err)
	}
	return nil
}

// Leave implements repository.RoomRepository.
func (r *PostgresRoomRepository) Leave(ctx context.Context, roomID, userID string) error {
	rid, err := parseRoomID(roomID)
	if err != nil {
		return fmt.Errorf("invalid room id: %w", err)
	}
	var one int
	if err := r.pool.QueryRow(ctx, `SELECT 1 FROM rooms WHERE id = $1`, rid).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrRoomNotFound
		}
		return fmt.Errorf("room lookup: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM votes WHERE room_id = $1 AND user_id = $2`, rid, userID); err != nil {
		return fmt.Errorf("delete votes: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM room_participants WHERE room_id = $1 AND user_id = $2`, rid, userID); err != nil {
		return fmt.Errorf("delete participant: %w", err)
	}
	return nil
}
