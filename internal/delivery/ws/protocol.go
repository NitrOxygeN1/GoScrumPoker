package ws

import "encoding/json"

// Wire format (JSON): every WebSocket message uses this envelope:
//
//	{ "type": "<kind>", "payload": { ... } }
//
// Inbound (client → server) kinds: join, vote, reveal, reset.
// Outbound (server → client) kinds: state, error.

// Client message types (inbound).
const (
	MsgJoin   = "join"
	MsgVote   = "vote"
	MsgReveal = "reveal"
	MsgReset  = "reset"
)

// Server message types (outbound).
const (
	MsgState = "state"
	MsgError = "error"
)

// ClientMessage is the canonical inbound JSON shape.
type ClientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ServerMessage is the canonical outbound JSON shape.
type ServerMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Envelope is an alias for ClientMessage.
type Envelope = ClientMessage

// JoinPayload is required for the first "join" message.
// Avatar is optional (e.g. Google profile picture) and surfaces in the room snapshot.
type JoinPayload struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// VotePayload carries the chosen card value.
type VotePayload struct {
	Value string `json:"value"`
}

// ErrorPayload describes a rejected client action.
type ErrorPayload struct {
	Message string `json:"message"`
}
