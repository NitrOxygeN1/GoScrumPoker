package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
)

// rowQuerier matches *pgxpool.Pool and pgx.Tx for read helpers.
type rowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func parseRoomID(roomID string) (uuid.UUID, error) {
	return uuid.Parse(roomID)
}

func loadRoom(ctx context.Context, q rowQuerier, roomID uuid.UUID) (*domain.Room, error) {
	var revealed bool
	err := q.QueryRow(ctx, `SELECT revealed FROM rooms WHERE id = $1`, roomID).Scan(&revealed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrRoomNotFound
		}
		return nil, fmt.Errorf("load room row: %w", err)
	}

	r := domain.NewRoom(roomID.String())
	r.Revealed = revealed

	rows, err := q.Query(ctx,
		`SELECT user_id, display_name FROM room_participants WHERE room_id = $1 ORDER BY user_id`,
		roomID,
	)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid, display string
		if err := rows.Scan(&uid, &display); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		r.Users[uid] = domain.User{ID: uid, Name: display}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	vrows, err := q.Query(ctx,
		`SELECT user_id, value FROM votes WHERE room_id = $1`,
		roomID,
	)
	if err != nil {
		return nil, fmt.Errorf("list votes: %w", err)
	}
	defer vrows.Close()

	for vrows.Next() {
		var uid, value string
		if err := vrows.Scan(&uid, &value); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		r.Votes[uid] = value
	}
	if err := vrows.Err(); err != nil {
		return nil, err
	}

	return r, nil
}
