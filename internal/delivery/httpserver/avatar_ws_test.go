package httpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	apihttp "GoScrumPoker/internal/delivery/httpserver"
	"GoScrumPoker/internal/delivery/ws"
	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/repository"
	"GoScrumPoker/internal/service"
)

// Ensures the avatar from JoinPayload flows through the room snapshot so the
// SPA can render Google profile pictures next to user names.
func TestWebSocket_avatarRoundTripsThroughSnapshot(t *testing.T) {
	log := zerolog.New(io.Discard)
	mem := repository.NewMemory()
	roomSvc := service.NewRoomService(mem)
	voteSvc := service.NewVoteService(mem)
	hub := ws.NewHub(roomSvc, voteSvc, log)
	go hub.Run()

	h := apihttp.NewRouter(apihttp.Dependencies{
		Log:                     log,
		Rooms:                   roomSvc,
		Votes:                   voteSvc,
		Hub:                     hub,
		Auth:                    nil,
		DBBackend:               "memory",
		DBPing:                  nil,
		HealthExposeErrorDetail: true,
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer mem.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	roomID := func() string {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/rooms", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create room: status %d", resp.StatusCode)
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.ID
	}()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + roomID
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	join := map[string]any{
		"type": "join",
		"payload": map[string]string{
			"user_id": "avatar-user",
			"name":    "Avatar Tester",
			"avatar":  "https://example.invalid/avatar.png",
		},
	}
	b, _ := json.Marshal(join)
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Type != "state" {
			continue
		}
		var snap domain.RoomState
		if err := json.Unmarshal(env.Payload, &snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.Users) == 0 {
			continue
		}
		got := snap.Users[0]
		if got.Avatar != "https://example.invalid/avatar.png" {
			t.Fatalf("avatar lost in snapshot: got %q", got.Avatar)
		}
		return
	}
	t.Fatal("timed out waiting for room state with avatar")
}
