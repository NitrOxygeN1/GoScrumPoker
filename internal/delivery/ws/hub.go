package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/rs/zerolog"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
	svc "GoScrumPoker/internal/service"
)

const (
	hubEventBuffer = 256
	hubLeaveBuffer = 64
)

// ClientEvent is a parsed inbound WebSocket action handed off to the hub.
type ClientEvent struct {
	Type    string
	RoomID  string
	Client  *Client
	Payload json.RawMessage
}

type leaveOp struct {
	roomID string
	client *Client
	done   chan struct{}
}

// Hub multiplexes rooms and delegates persistence to application services.
type Hub struct {
	rooms   *svc.RoomService
	votes   *svc.VoteService
	baseCtx context.Context
	logger zerolog.Logger

	events chan ClientEvent
	leave  chan leaveOp

	wg sync.WaitGroup

	trackMu sync.Mutex
	tracked map[*Client]struct{}
}

// NewHub constructs a hub. Call Run in a dedicated goroutine before accepting traffic.
func NewHub(rooms *svc.RoomService, votes *svc.VoteService, log zerolog.Logger) *Hub {
	return &Hub{
		rooms:   rooms,
		votes:   votes,
		baseCtx: context.Background(),
		logger:  log,
		events:  make(chan ClientEvent, hubEventBuffer),
		leave:   make(chan leaveOp, hubLeaveBuffer),
		tracked: make(map[*Client]struct{}),
	}
}

// Track registers an active WebSocket client for shutdown-time cleanup.
func (h *Hub) Track(c *Client) {
	h.trackMu.Lock()
	defer h.trackMu.Unlock()
	h.tracked[c] = struct{}{}
}

// Untrack removes a client after its connection has fully ended.
func (h *Hub) Untrack(c *Client) {
	h.trackMu.Lock()
	defer h.trackMu.Unlock()
	delete(h.tracked, c)
}

// Shutdown closes all tracked WebSocket connections so HTTP handlers can return.
func (h *Hub) Shutdown(ctx context.Context) {
	h.trackMu.Lock()
	list := make([]*Client, 0, len(h.tracked))
	for c := range h.tracked {
		list = append(list, c)
	}
	h.trackMu.Unlock()

	for _, c := range list {
		if ctx.Err() != nil {
			return
		}
		c.CloseConn()
	}
}

// Run processes room events until the process exits.
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

// Submit enqueues a client-originated action.
func (h *Hub) Submit(ev ClientEvent) error {
	select {
	case h.events <- ev:
		return nil
	default:
		return errors.New("hub overloaded")
	}
}

// Leave blocks until the hub has applied disconnect bookkeeping.
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
		ev.Client.logger.Warn().Str("ws_event", ev.Type).Str("room_id", ev.RoomID).Msg("unknown websocket message type")
		ev.Client.enqueue(serverErrorBytes("unknown message type"))
	}
}

func (h *Hub) onJoin(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	var jp JoinPayload
	if err := json.Unmarshal(ev.Payload, &jp); err != nil || jp.UserID == "" || jp.Name == "" {
		ev.Client.enqueue(serverErrorBytes("join requires user_id and name"))
		return
	}

	if err := h.rooms.JoinRoom(h.baseCtx, ev.RoomID, domain.User{ID: jp.UserID, Name: jp.Name}); err != nil {
		if errors.Is(err, repository.ErrRoomNotFound) {
			ev.Client.logger.Warn().Str("ws_event", MsgJoin).Str("room_id", ev.RoomID).Msg("join room not found")
			ev.Client.enqueue(serverErrorBytes("room not found"))
			return
		}
		if errors.Is(err, svc.ErrInvalidInput) {
			ev.Client.logger.Warn().Str("ws_event", MsgJoin).Str("room_id", ev.RoomID).Msg("join invalid input")
			ev.Client.enqueue(serverErrorBytes("invalid join payload"))
			return
		}
		ev.Client.logger.Error().Err(err).Str("ws_event", MsgJoin).Str("room_id", ev.RoomID).Msg("join failed")
		ev.Client.enqueue(serverErrorBytes("unable to join room"))
		return
	}

	ev.Client.setJoined(jp.UserID)
	h.addClient(rooms, ev.RoomID, ev.Client)
	h.broadcastState(rooms, ev.RoomID)
	ev.Client.logger.Info().
		Str("ws_event", MsgJoin).
		Str("room_id", ev.RoomID).
		Str("user_id", jp.UserID).
		Msg("websocket event")
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

	if err := h.votes.PlaceVote(h.baseCtx, ev.RoomID, uid, vp.Value); err != nil {
		lg := ev.Client.logger.With().Str("ws_event", MsgVote).Str("room_id", ev.RoomID).Str("user_id", uid).Logger()
		switch {
		case errors.Is(err, repository.ErrRoomNotFound):
			lg.Warn().Msg("vote rejected: room not found")
			ev.Client.enqueue(serverErrorBytes("room not found"))
		case errors.Is(err, repository.ErrUserNotInRoom):
			lg.Warn().Msg("vote rejected: user not in room")
			ev.Client.enqueue(serverErrorBytes("user not in room"))
		case errors.Is(err, repository.ErrVotesRevealed):
			lg.Warn().Msg("vote rejected: votes revealed")
			ev.Client.enqueue(serverErrorBytes("votes are revealed; reset before voting again"))
		case errors.Is(err, repository.ErrInvalidVote):
			lg.Warn().Msg("vote rejected: invalid value")
			ev.Client.enqueue(serverErrorBytes("invalid vote value"))
		default:
			lg.Error().Err(err).Msg("vote failed")
			ev.Client.enqueue(serverErrorBytes(err.Error()))
		}
		return
	}

	h.broadcastState(rooms, ev.RoomID)
	ev.Client.logger.Info().
		Str("ws_event", MsgVote).
		Str("room_id", ev.RoomID).
		Str("user_id", uid).
		Msg("websocket event")
}

func (h *Hub) onReveal(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	if _, ok := ev.Client.joinedUser(); !ok {
		ev.Client.enqueue(serverErrorBytes("join before reveal"))
		return
	}
	if err := h.votes.RevealVotes(h.baseCtx, ev.RoomID); err != nil {
		ev.Client.logger.Error().Err(err).Str("room_id", ev.RoomID).Str("ws_event", MsgReveal).Msg("websocket reveal failed")
		ev.Client.enqueue(serverErrorBytes("unable to reveal"))
		return
	}
	h.broadcastState(rooms, ev.RoomID)
	ev.Client.logger.Info().Str("ws_event", MsgReveal).Str("room_id", ev.RoomID).Msg("websocket event")
}

func (h *Hub) onReset(rooms map[string]map[*Client]struct{}, ev ClientEvent) {
	if _, ok := ev.Client.joinedUser(); !ok {
		ev.Client.enqueue(serverErrorBytes("join before reset"))
		return
	}
	if err := h.votes.ResetRound(h.baseCtx, ev.RoomID); err != nil {
		ev.Client.logger.Error().Err(err).Str("room_id", ev.RoomID).Str("ws_event", MsgReset).Msg("websocket reset failed")
		ev.Client.enqueue(serverErrorBytes("unable to reset"))
		return
	}
	h.broadcastState(rooms, ev.RoomID)
	ev.Client.logger.Info().Str("ws_event", MsgReset).Str("room_id", ev.RoomID).Msg("websocket event")
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

	if err := h.rooms.LeaveRoom(h.baseCtx, op.roomID, uid); err != nil {
		op.client.logger.Warn().Err(err).Str("room_id", op.roomID).Str("user_id", uid).Str("ws_event", "leave").Msg("websocket leave persistence failed")
		return
	}

	op.client.logger.Info().Str("ws_event", "leave").Str("room_id", op.roomID).Str("user_id", uid).Msg("websocket event")
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
	snap, ok, err := h.rooms.GetSnapshot(h.baseCtx, roomID)
	if err != nil {
		h.logger.Error().Err(err).Str("room_id", roomID).Msg("room snapshot failed")
		return
	}
	if !ok {
		return
	}
	msg := ServerMessage{Type: MsgState, Payload: snap}
	b, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error().Err(err).Str("room_id", roomID).Msg("marshal room state failed")
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
