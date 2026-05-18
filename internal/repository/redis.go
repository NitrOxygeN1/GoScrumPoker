package repository

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"GoScrumPoker/internal/domain"
)

const (
	redisKeyPrefix     = "scrum:room:"
	redisMeetKeyPrefix = "scrum:meet:" // value = roomID; bound to a Meet meetingId
)

type roomDoc struct {
	ID       string                 `json:"id"`
	Users    map[string]domain.User `json:"users"`
	Votes    map[string]string      `json:"votes"`
	Revealed bool                   `json:"revealed"`
}

// Redis implements RoomRepository and VoteRepository backed by Redis.
type Redis struct {
	rdb *redis.Client
	ttl time.Duration
}

var _ RoomRepository = (*Redis)(nil)
var _ VoteRepository = (*Redis)(nil)

// NewRedis builds a Redis-backed repository.
func NewRedis(rdb *redis.Client, ttl time.Duration) *Redis {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Redis{rdb: rdb, ttl: ttl}
}

func (s *Redis) roomKey(id string) string {
	return redisKeyPrefix + id
}

func (s *Redis) meetKey(meetingID string) string {
	return redisMeetKeyPrefix + meetingID
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

func copyRoomIntoDoc(r *domain.Room, doc *roomDoc) {
	doc.ID = r.ID
	doc.Revealed = r.Revealed
	doc.Users = maps.Clone(r.Users)
	doc.Votes = maps.Clone(r.Votes)
}

// CreateRoom implements RoomRepository.
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

// GetOrCreateRoomByMeet implements RoomRepository. The meeting->room binding
// is stored in a separate key so a stale binding (e.g. its target room expired)
// can be replaced atomically. Existing bindings have their TTL refreshed.
func (s *Redis) GetOrCreateRoomByMeet(ctx context.Context, meetingID string) (string, bool, error) {
	mkey := s.meetKey(meetingID)

	for attempt := 0; attempt < 8; attempt++ {
		existing, err := s.rdb.GetEx(ctx, mkey, s.ttl).Result()
		if err != nil && err != redis.Nil {
			return "", false, err
		}
		if err == nil && existing != "" {
			alive, exErr := s.Exists(ctx, existing)
			if exErr != nil {
				return "", false, exErr
			}
			if alive {
				return existing, false, nil
			}
			// Target room expired; drop the stale binding and fall through to create.
			if _, dErr := s.rdb.Del(ctx, mkey).Result(); dErr != nil {
				return "", false, dErr
			}
		}

		room, cErr := s.CreateRoom(ctx)
		if cErr != nil {
			return "", false, cErr
		}
		ok, sErr := s.rdb.SetNX(ctx, mkey, room.ID, s.ttl).Result()
		if sErr != nil {
			return "", false, sErr
		}
		if ok {
			return room.ID, true, nil
		}
		// Another caller bound the meeting first: drop the orphan we just created and retry.
		_, _ = s.rdb.Del(ctx, s.roomKey(room.ID)).Result()
	}

	return "", false, errors.New("could not bind meeting to a room: retry budget exhausted")
}

// Exists implements RoomRepository.
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

// Snapshot implements RoomRepository.
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

// Join implements RoomRepository.
func (s *Redis) Join(ctx context.Context, roomID string, user domain.User) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		doc.Users[user.ID] = user
		return nil
	})
}

// Leave implements RoomRepository.
func (s *Redis) Leave(ctx context.Context, roomID, userID string) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		delete(doc.Users, userID)
		delete(doc.Votes, userID)
		return nil
	})
}

// Apply implements VoteRepository.
func (s *Redis) Apply(ctx context.Context, roomID string, fn func(*domain.Room) error) error {
	return s.withRoom(ctx, roomID, func(doc *roomDoc) error {
		r := docToRoom(doc)
		if err := fn(r); err != nil {
			return err
		}
		copyRoomIntoDoc(r, doc)
		return nil
	})
}
