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

// VerifyMeetSchema confirms migration 000002 has been applied. Returns a
// descriptive error when the Meet binding column or avatar column is missing;
// callers can use the result to log a clear "run migrations" warning rather
// than failing at request time with an opaque SQLSTATE 42703.
func (r *PostgresRoomRepository) VerifyMeetSchema(ctx context.Context) error {
	var ok bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'rooms' AND column_name = 'meet_meeting_id'
		)`,
	).Scan(&ok); err != nil {
		return fmt.Errorf("verify meet schema: %w", err)
	}
	if !ok {
		return errors.New("rooms.meet_meeting_id is missing — run migration 000002_meet_and_avatar (RUN_MIGRATIONS_ON_STARTUP=true) before using the Meet add-on")
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'room_participants' AND column_name = 'avatar_url'
		)`,
	).Scan(&ok); err != nil {
		return fmt.Errorf("verify avatar schema: %w", err)
	}
	if !ok {
		return errors.New("room_participants.avatar_url is missing — run migration 000002_meet_and_avatar (RUN_MIGRATIONS_ON_STARTUP=true) before joining a Meet room")
	}

	// Verify the UNIQUE index on rooms.meet_meeting_id is non-partial. A
	// partial index (the original 000002 shape) silently breaks ON CONFLICT
	// inference at runtime with SQLSTATE 42P10. Migration 000003 replaces it.
	var hasUsableIndex bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM pg_index i
			JOIN pg_class c ON c.oid = i.indexrelid
			JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_attribute a
			  ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
			WHERE t.relname = 'rooms'
			  AND a.attname = 'meet_meeting_id'
			  AND i.indisunique = true
			  AND i.indpred IS NULL
		)`,
	).Scan(&hasUsableIndex); err != nil {
		return fmt.Errorf("verify meet unique index: %w", err)
	}
	if !hasUsableIndex {
		return errors.New("rooms.meet_meeting_id has no non-partial UNIQUE index — apply migration 000003_meet_meeting_id_full_unique (INSERT ... ON CONFLICT cannot infer from a partial index)")
	}
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

// GetOrCreateRoomByMeet implements repository.RoomRepository.
// The (rooms.meet_meeting_id) partial unique index plus ON CONFLICT DO NOTHING
// serializes concurrent first-launches of the same Meet add-on instance.
func (r *PostgresRoomRepository) GetOrCreateRoomByMeet(ctx context.Context, meetingID string) (string, bool, error) {
	var existing uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM rooms WHERE meet_meeting_id = $1`, meetingID,
	).Scan(&existing)
	if err == nil {
		return existing.String(), false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("lookup meet room: %w", err)
	}

	newID := uuid.New()
	var insertedID uuid.UUID
	err = r.pool.QueryRow(ctx,
		`INSERT INTO rooms (id, meet_meeting_id) VALUES ($1, $2)
		 ON CONFLICT (meet_meeting_id) DO NOTHING
		 RETURNING id`,
		newID, meetingID,
	).Scan(&insertedID)
	switch {
	case err == nil:
		return insertedID.String(), true, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Lost the insert race; fetch the winner.
		if err := r.pool.QueryRow(ctx,
			`SELECT id FROM rooms WHERE meet_meeting_id = $1`, meetingID,
		).Scan(&existing); err != nil {
			return "", false, fmt.Errorf("re-fetch meet room: %w", err)
		}
		return existing.String(), false, nil
	default:
		return "", false, fmt.Errorf("insert meet room: %w", err)
	}
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
//
// The upsert and the ghost-eviction run in a single transaction so the room
// never momentarily contains both the stale and the fresh participant row.
// Eviction targets are: any other participant in the same room whose
// display_name (trim+lower) matches the incoming user. This collapses the
// duplicate left behind when a service restart kills the previous WebSocket
// without running Leave and the returning user reconnects with a new
// anonymous user_id (sessionStorage cleared, mobile background eviction,
// etc.). Match is intentionally name-based — there is no other stable
// identity available for anonymous joiners.
func (r *PostgresRoomRepository) Join(ctx context.Context, roomID string, user domain.User) error {
	rid, err := parseRoomID(roomID)
	if err != nil {
		return fmt.Errorf("invalid room id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin join tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM rooms WHERE id = $1`, rid).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.ErrRoomNotFound
		}
		return fmt.Errorf("room lookup: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO room_participants (room_id, user_id, display_name, avatar_url) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (room_id, user_id) DO UPDATE SET
		     display_name = EXCLUDED.display_name,
		     avatar_url   = EXCLUDED.avatar_url`,
		rid, user.ID, user.Name, user.Avatar,
	); err != nil {
		return fmt.Errorf("join participant: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM votes
		 WHERE room_id = $1
		   AND user_id <> $2
		   AND user_id IN (
		       SELECT user_id FROM room_participants
		       WHERE room_id = $1
		         AND user_id <> $2
		         AND lower(btrim(display_name)) = lower(btrim($3))
		   )`,
		rid, user.ID, user.Name,
	); err != nil {
		return fmt.Errorf("evict duplicate votes: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM room_participants
		 WHERE room_id = $1
		   AND user_id <> $2
		   AND lower(btrim(display_name)) = lower(btrim($3))`,
		rid, user.ID, user.Name,
	); err != nil {
		return fmt.Errorf("evict duplicate participants: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit join: %w", err)
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
