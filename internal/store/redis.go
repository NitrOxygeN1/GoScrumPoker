package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"GoScrumPoker/internal/domain"
)

const redisKeyPrefix = "scrum:room:"

type roomDoc struct {
	ID       string                 `json:"id"`
	Users    map[string]domain.User `json:"users"`
	Votes    map[string]string      `json:"votes"`
	Revealed bool                   `json:"revealed"`
}

// Redis stores room documents in Redis with a sliding TTL on each access and mutation.
type Redis struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewRedis builds a RoomStore backed by Redis. ttl is applied on create and refreshed on reads/writes.
func NewRedis(rdb *redis.Client, ttl time.Duration) *Redis {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Redis{rdb: rdb, ttl: ttl}
}

func (s *Redis) roomKey(id string) string {
	return redisKeyPrefix + id
}

// Close closes the Redis client.
func (s *Redis) Close() error {
	return s.rdb.Close()
}

func normalizeDoc(d *roomDoc) {
	if d.Users == nil {
		d.Users = make(map[string]domain.User)
	}
	if d.Votes == nil {
		d.Votes = make(map[string]string)
	}
}

func docToRoom(d *roomDoc) *domain.Room {
	normalizeDoc(d)
	r := domain.NewRoom(d.ID)
	r.Revealed = d.Revealed
	for k, v := range d.Users {
		r.Users[k] = v
	}
	for k, v := range d.Votes {
		r.Votes[k] = v
	}
	return r
}

// CreateRoom persists a new empty room.
func (s *Redis) CreateRoom(ctx context.Context) (*domain.Room, error) {
	for i := 0; i < 8; i++ {
		id := uuid.NewString()
		key := s.roomKey(id)
		doc := roomDoc{
			ID:       id,
			Users:    make(map[string]domain.User),
			Votes:    make(map[string]string),
			Revealed: false,
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		ok, err := s.rdb.SetNX(ctx, key, b, s.ttl).Result()
		if err != nil {
			return nil, err
		}
		if ok {
			return domain.NewRoom(id), nil
		}
	}
	return nil, errors.New("could not allocate unique room id")
}

// Exists reports whether a room exists and refreshes its TTL when it does.
func (s *Redis) Exists(ctx context.Context, id string) (bool, error) {
	key := s.roomKey(id)
	n, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		_ = s.rdb.Expire(ctx, key, s.ttl).Err()
	}
	return n == 1, nil
}

// Snapshot returns room state and extends TTL (GETEX).
func (s *Redis) Snapshot(ctx context.Context, id string) (domain.RoomState, bool, error) {
	key := s.roomKey(id)
	raw, err := s.rdb.GetEx(ctx, key, s.ttl).Bytes()
	if err == redis.Nil {
		return domain.RoomState{}, false, nil
	}
	if err != nil {
		return domain.RoomState{}, false, err
	}
	var doc roomDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return domain.RoomState{}, false, err
	}
	normalizeDoc(&doc)
	return domain.BuildRoomState(docToRoom(&doc)), true, nil
}

func (s *Redis) withRoom(ctx context.Context, roomID string, fn func(*roomDoc) error) error {
	key := s.roomKey(roomID)
	const maxAttempts = 24
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := s.rdb.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Bytes()
			if err == redis.Nil {
				return ErrRoomNotFound
			}
			if err != nil {
				return err
			}
			var doc roomDoc
			if err := json.Unmarshal(raw, &doc); err != nil {
				return err
			}
			normalizeDoc(&doc)
			if err := fn(&doc); err != nil {
				return err
			}
			out, err := json.Marshal(doc)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.Set(ctx, key, out, s.ttl)
				return nil
			})
			return err
		}, key)
		if err == nil {
			return nil
		}
		if errors.Is(err, redis.TxFailedErr) {
			lastErr = err
			continue
		}
		return err
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("room update conflict: retry budget exhausted")
}

// Join adds or updates a user.
func (s *Redis) Join(ctx context.Context, roomID string, user domain.User) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		doc.Users[user.ID] = user
		return nil
	})
}

// Vote records or updates a vote.
func (s *Redis) Vote(ctx context.Context, roomID, userID, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return ErrInvalidVote
	}
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		if _, ok := doc.Users[userID]; !ok {
			return ErrUserNotInRoom
		}
		if doc.Revealed {
			return ErrVotesRevealed
		}
		doc.Votes[userID] = value
		return nil
	})
}

// Reveal sets revealed flag.
func (s *Redis) Reveal(ctx context.Context, roomID string) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		doc.Revealed = true
		return nil
	})
}

// Reset clears votes and hides them.
func (s *Redis) Reset(ctx context.Context, roomID string) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		doc.Revealed = false
		doc.Votes = make(map[string]string)
		return nil
	})
}

// Leave removes a user and their vote.
func (s *Redis) Leave(ctx context.Context, roomID, userID string) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		delete(doc.Users, userID)
		delete(doc.Votes, userID)
		return nil
	})
}
