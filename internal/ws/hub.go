package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/store"
)

const (
	hubEventBuffer = 256
	hubLeaveBuffer = 64
)

// ClientEvent is a parsed inbound WebSocket action handed off to the hub.
// The hub is the single goroutine that mutates room membership and fans out broadcasts.
type ClientEvent struct {
	Type    string
	RoomID  string
	Client  *Client
	Payload json.RawMessage
}

type leaveOp struct {
	roomID string
	client *Client
	done   chan struct{} // closed after removal and optional broadcast
}

// Hub multiplexes many Scrum Poker rooms: registration, leave, and broadcasts
// are processed sequentially per hub goroutine (no mutex on the room→clients map).
type Hub struct {
	store   store.RoomStore
	baseCtx context.Context
	logger  *slog.Logger

	events chan ClientEvent
	leave  chan leaveOp

	wg sync.WaitGroup
}

// NewHub constructs a hub. Call Run in a dedicated goroutine before accepting traffic.
func NewHub(st store.RoomStore, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		store:   st,
		baseCtx: context.Background(),
		logger:  log,
		events:  make(chan ClientEvent, hubEventBuffer),
		leave:   make(chan leaveOp, hubLeaveBuffer),
	}
}

// Run processes room events until the process exits. Intended pattern: go hub.Run().
func (h *Hub) Run() {
	h.wg.Add(1)
	defer h.wg.Done()

	rooms := make(map[string]map[*Client]struct{})

	for {
		select {
		case ev := <-h.events:
			h.handleEvent(rooms, ev)
		case op := <-h.leave:
			h.handleLeave(rooms, op)
		}
	}
}

// Submit enqueues a client-originated action (join, vote, reveal, reset).
func (h *Hub) Submit(ev ClientEvent) error {
	select {
	case h.events <- ev:
		return nil
	default:
		return errors.New("hub overloaded")
	}
}

// Leave removes a client from its room and updates domain state. It blocks until
// the hub has applied the leave, so callers can safely close the outbound channel after.
func (h *Hub) Leave(roomID string, c *Client) {
	done := make(chan struct{})
	h.leave <- leaveOp{roomID: roomID, client: c, done: done}
	<-done
}

func (h *Hub) handleEvent(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	switch ev.Type {
	case MsgJoin:
		h.onJoin(rooms, ev)
	case MsgVote:
		h.onVote(rooms, ev)
	case MsgReveal:
		h.onReveal(rooms, ev)
	case MsgReset:
		h.onReset(rooms, ev)
	default:
		ev.Client.enqueue(serverErrorBytes("unknown message type"))
	}
}

func (h *Hub) onJoin(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	var jp JoinPayload
	if err := json.Unmarshal(ev.Payload, &jp); err != nil || jp.UserID == "" || jp.Name == "" {
		ev.Client.enqueue(serverErrorBytes("join requires user_id and name"))
		return
	}

	if err := h.store.Join(h.baseCtx, ev.RoomID, domain.User{ID: jp.UserID, Name: jp.Name}); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			ev.Client.enqueue(serverErrorBytes("room not found"))
			return
		}
		ev.Client.enqueue(serverErrorBytes("unable to join room"))
		return
	}

	ev.Client.setJoined(jp.UserID)
	h.addClient(rooms, ev.RoomID, ev.Client)
	h.broadcastState(rooms, ev.RoomID)
	h.logger.Info("user joined room", "room_id", ev.RoomID, "user_id", jp.UserID)
}

func (h *Hub) onVote(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	uid, ok := ev.Client.joinedUser()
	if !ok {
		ev.Client.enqueue(serverErrorBytes("join before voting"))
		return
	}

	var vp VotePayload
	if err := json.Unmarshal(ev.Payload, &vp); err != nil {
		ev.Client.enqueue(serverErrorBytes("invalid vote payload"))
		return
	}

	if err := h.store.Vote(h.baseCtx, ev.RoomID, uid, vp.Value); err != nil {
		switch {
		case errors.Is(err, store.ErrRoomNotFound):
			ev.Client.enqueue(serverErrorBytes("room not found"))
		case errors.Is(err, store.ErrUserNotInRoom):
			ev.Client.enqueue(serverErrorBytes("user not in room"))
		case errors.Is(err, store.ErrVotesRevealed):
			ev.Client.enqueue(serverErrorBytes("votes are revealed; reset before voting again"))
		case errors.Is(err, store.ErrInvalidVote):
			ev.Client.enqueue(serverErrorBytes("invalid vote value"))
		default:
			ev.Client.enqueue(serverErrorBytes(err.Error()))
		}
		return
	}

	h.broadcastState(rooms, ev.RoomID)
}

func (h *Hub) onReveal(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	if _, ok := ev.Client.joinedUser(); !ok {
		ev.Client.enqueue(serverErrorBytes("join before reveal"))
		return
	}
	if err := h.store.Reveal(h.baseCtx, ev.RoomID); err != nil {
		ev.Client.enqueue(serverErrorBytes("unable to reveal"))
		return
	}
	h.broadcastState(rooms, ev.RoomID)
}

func (h *Hub) onReset(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	if _, ok := ev.Client.joinedUser(); !ok {
		ev.Client.enqueue(serverErrorBytes("join before reset"))
		return
	}
	if err := h.store.Reset(h.baseCtx, ev.RoomID); err != nil {
		ev.Client.enqueue(serverErrorBytes("unable to reset"))
		return
	}
	h.broadcastState(rooms, ev.RoomID)
}

func (h *Hub) handleLeave(rooms map[string]map[*Client]struct{}, op leaveOp) {
	defer func() {
		if op.done != nil {
			close(op.done)
		}
	}()

	h.removeClient(rooms, op.roomID, op.client)

	uid, joined := op.client.joinedUser()
	if !joined {
		return
	}

	if err := h.store.Leave(h.baseCtx, op.roomID, uid); err != nil {
		h.logger.Warn("leave failed", "room_id", op.roomID, "user_id", uid, "err", err)
		return
	}

	h.logger.Info("user left room", "room_id", op.roomID, "user_id", uid)
	h.broadcastState(rooms, op.roomID)
}

func (h *Hub) addClient(rooms map[string]map[*Client]struct{}, roomID string, c *Client) {
	set := rooms[roomID]
	if set == nil {
		set = make(map[*Client]struct{})
		rooms[roomID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) removeClient(rooms map[string]map[*Client]struct{}, roomID string, c *Client) {
	set, ok := rooms[roomID]
	if !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(rooms, roomID)
	}
}

func (h *Hub) broadcastState(rooms map[string]map[*Client]struct{}, roomID string) {
	snap, ok, err := h.store.Snapshot(h.baseCtx, roomID)
	if err != nil {
		h.logger.Error("room snapshot failed", "room_id", roomID, "err", err)
		return
	}
	if !ok {
		return
	}
	msg := ServerMessage{Type: MsgState, Payload: snap}
	b, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("marshal room state", "err", err)
		return
	}
	h.fanout(rooms, roomID, b)
}

func (h *Hub) fanout(rooms map[string]map[*Client]struct{}, roomID string, payload []byte) {
	set := rooms[roomID]
	for c := range set {
		c.enqueue(payload)
	}
}

func serverErrorBytes(message string) []byte {
	msg := ServerMessage{Type: MsgError, Payload: ErrorPayload{Message: message}}
	b, _ := json.Marshal(msg)
	return b
}
