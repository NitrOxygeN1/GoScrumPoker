package domain

import (
	"sort"
	"strings"
)

// SameDisplayName reports whether two display names should be treated as the
// same participant for de-duplication. Comparison is case-insensitive and
// ignores leading/trailing whitespace. An empty (trimmed) name never matches,
// because clients must supply a real name on join.
//
// Used to collapse "ghost" participants left behind when a service restart
// (e.g. Render's idle cold-shutdown) prevents the normal Leave path from
// running and the returning user reconnects with a fresh anonymous user_id.
func SameDisplayName(a, b string) bool {
	at := strings.TrimSpace(a)
	bt := strings.TrimSpace(b)
	if at == "" || bt == "" {
		return false
	}
	return strings.EqualFold(at, bt)
}

// User is a participant in a planning poker room.
// Avatar is an optional image URL (e.g. Google profile picture) used for UI only;
// it has no role in identity or authorization.
type User struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// Room is the aggregate root for a Scrum Poker session.
// Mutations must go through the store so locking stays consistent.
type Room struct {
	ID       string
	Users    map[string]User
	Votes    map[string]string // userID -> face value (e.g. "5", "?")
	Revealed bool
}

// NewRoom constructs an empty room with initialized maps.
func NewRoom(id string) *Room {
	return &Room{
		ID:       id,
		Users:    make(map[string]User),
		Votes:    make(map[string]string),
		Revealed: false,
	}
}

// RoomState is a JSON-safe snapshot for HTTP and WebSocket broadcasts.
type RoomState struct {
	ID       string            `json:"id"`
	Revealed bool              `json:"revealed"`
	Users    []UserPresence    `json:"users"`
	Votes    map[string]string `json:"votes,omitempty"`
}

// UserPresence augments a user with voting progress without leaking values when hidden.
// Avatar is omitted when not set so the wire format stays backwards-compatible with
// older clients that never sent or rendered avatars.
type UserPresence struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
	Voted  bool   `json:"voted"`
}

// BuildRoomState returns a snapshot suitable for clients.
// Votes are included only after the facilitator reveals them.
func BuildRoomState(r *Room) RoomState {
	st := RoomState{
		ID:       r.ID,
		Revealed: r.Revealed,
		Users:    make([]UserPresence, 0, len(r.Users)),
	}

	ids := make([]string, 0, len(r.Users))
	for id := range r.Users {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, uid := range ids {
		u := r.Users[uid]
		_, has := r.Votes[uid]
		st.Users = append(st.Users, UserPresence{
			ID:     u.ID,
			Name:   u.Name,
			Avatar: u.Avatar,
			Voted:  has,
		})
	}

	if r.Revealed {
		st.Votes = make(map[string]string, len(r.Votes))
		for uid, v := range r.Votes {
			st.Votes[uid] = v
		}
	}

	return st
}
