package ws

import (
	"context"
	"encoding/json"
	"log/slog"

	"GoScrumPoker/internal/store"
)

func broadcastRoomState(log *slog.Logger, st *store.Memory, reg *Registry, roomID string) {
	snap, ok, err := st.Snapshot(context.Background(), roomID)
	if err != nil || !ok {
		return
	}
	msg := struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}{Type: MsgState, Payload: snap}
	b, err := json.Marshal(msg)
	if err != nil {
		log.Error("marshal room state", "err", err)
		return
	}
	reg.Broadcast(roomID, b)
}

func serverError(message string) []byte {
	msg := struct {
		Type    string       `json:"type"`
		Payload ErrorPayload `json:"payload"`
	}{Type: MsgError, Payload: ErrorPayload{Message: message}}
	b, _ := json.Marshal(msg)
	return b
}
