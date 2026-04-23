package httpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"GoScrumPoker/internal/domain"
	"GoScrumPoker/internal/httpserver"
	"GoScrumPoker/internal/store"
	"GoScrumPoker/internal/ws"
)

type serverEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func newTestServer(t *testing.T) (srv *httptest.Server, cleanup func()) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	st := store.NewMemory()
	hub := ws.NewHub(st, log)
	go hub.Run()

	h := httpserver.NewRouter(httpserver.Dependencies{
		Log:   log,
		Store: st,
		Hub:   hub,
		Auth:  nil,
	})
	srv = httptest.NewServer(h)
	cleanup = func() {
		srv.Close()
		_ = st.Close()
	}
	return srv, cleanup
}

func wsURL(httpURL, roomID string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws/" + roomID
}

func createRoom(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/rooms", nil)
	if err != nil {
		t.Fatal(err)
	}
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
	if body.ID == "" {
		t.Fatal("empty room id")
	}
	return body.ID
}

func TestIntegration_RoomCreation(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	roomID := createRoom(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/rooms/"+roomID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: status %d", resp.StatusCode)
	}
	var snap domain.RoomState
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.ID != roomID {
		t.Fatalf("snap id: got %q want %q", snap.ID, roomID)
	}
	if snap.Revealed {
		t.Fatal("new room should not be revealed")
	}
	if len(snap.Users) != 0 {
		t.Fatalf("expected no users, got %d", len(snap.Users))
	}

	const workers = 12
	var wg sync.WaitGroup
	seen := sync.Map{}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := createRoom(t, srv)
			seen.Store(id, struct{}{})
		}()
	}
	wg.Wait()
	count := 0
	seen.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != workers {
		t.Fatalf("distinct rooms: got %d want %d", count, workers)
	}
}

func TestIntegration_WebSocketCommunication(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	roomID := createRoom(t, srv)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL(srv.URL, roomID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	msgs := make(chan serverEnvelope, 64)
	go func() {
		for {
			_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env serverEnvelope
			if json.Unmarshal(raw, &env) != nil {
				continue
			}
			msgs <- env
		}
	}()

	join := map[string]any{
		"type": "join",
		"payload": map[string]string{
			"user_id": "ws-test-user",
			"name":    "Tester",
		},
	}
	b, _ := json.Marshal(join)
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatal(err)
	}

	state := readStateWhen(t, msgs, func(s domain.RoomState) bool {
		return len(s.Users) == 1 && s.Users[0].ID == "ws-test-user"
	}, 5*time.Second)

	if state.Users[0].Name != "Tester" {
		t.Fatalf("name %q", state.Users[0].Name)
	}
}

func TestIntegration_VotingFlow(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	roomID := createRoom(t, srv)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}

	openClient := func(userID, name string) (*websocket.Conn, <-chan serverEnvelope) {
		t.Helper()
		c, _, err := dialer.Dial(wsURL(srv.URL, roomID), nil)
		if err != nil {
			t.Fatal(err)
		}
		out := make(chan serverEnvelope, 256)
		go func() {
			defer close(out)
			for {
				_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
				_, raw, err := c.ReadMessage()
				if err != nil {
					return
				}
				var env serverEnvelope
				if json.Unmarshal(raw, &env) != nil {
					continue
				}
				out <- env
			}
		}()
		payload, _ := json.Marshal(map[string]string{"user_id": userID, "name": name})
		msg, _ := json.Marshal(map[string]json.RawMessage{
			"type":    json.RawMessage(`"join"`),
			"payload": payload,
		})
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			t.Fatal(err)
		}
		return c, out
	}

	sendVote := func(c *websocket.Conn, value string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"value": value})
		msg, _ := json.Marshal(map[string]json.RawMessage{
			"type":    json.RawMessage(`"vote"`),
			"payload": payload,
		})
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			t.Fatal(err)
		}
	}

	sendReveal := func(c *websocket.Conn) {
		t.Helper()
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"reveal"}`)); err != nil {
			t.Fatal(err)
		}
	}

	connA, chA := openClient("voter-a", "Alice")
	defer connA.Close()
	readStateWhen(t, chA, func(s domain.RoomState) bool {
		return len(s.Users) >= 1
	}, 5*time.Second)

	connB, chB := openClient("voter-b", "Bob")
	defer connB.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readStateWhen(t, chA, func(s domain.RoomState) bool {
			return len(s.Users) >= 2
		}, 6*time.Second)
	}()
	go func() {
		defer wg.Done()
		readStateWhen(t, chB, func(s domain.RoomState) bool {
			return len(s.Users) >= 2
		}, 6*time.Second)
	}()
	wg.Wait()

	sendVote(connA, "5")
	readStateWhen(t, chA, func(s domain.RoomState) bool {
		return countVoted(s) >= 1 && !s.Revealed
	}, 5*time.Second)

	sendVote(connB, "8")
	readStateWhen(t, chA, func(s domain.RoomState) bool {
		return countVoted(s) == 2 && !s.Revealed
	}, 5*time.Second)

	sendReveal(connA)
	finalA := readStateWhen(t, chA, func(s domain.RoomState) bool {
		return s.Revealed && len(s.Votes) >= 2
	}, 5*time.Second)

	if finalA.Votes["voter-a"] != "5" || finalA.Votes["voter-b"] != "8" {
		t.Fatalf("votes A: %+v", finalA.Votes)
	}

	finalB := readStateWhen(t, chB, func(s domain.RoomState) bool {
		return s.Revealed && len(s.Votes) >= 2
	}, 5*time.Second)
	if finalB.Votes["voter-a"] != "5" || finalB.Votes["voter-b"] != "8" {
		t.Fatalf("votes B: %+v", finalB.Votes)
	}
}

// readStateWhen blocks until a "state" message satisfies pred or timeout.
func readStateWhen(t *testing.T, ch <-chan serverEnvelope, pred func(domain.RoomState) bool, timeout time.Duration) domain.RoomState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last domain.RoomState
	for time.Now().Before(deadline) {
		select {
		case env, ok := <-ch:
			if !ok {
				t.Fatal("websocket output channel closed early")
			}
			if env.Type == "error" {
				t.Fatalf("server error payload: %s", string(env.Payload))
			}
			if env.Type != "state" {
				continue
			}
			if err := json.Unmarshal(env.Payload, &last); err != nil {
				t.Fatal(err)
			}
			if pred(last) {
				return last
			}
		case <-time.After(25 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for room state (last users=%d voted=%d revealed=%v)",
		len(last.Users), countVoted(last), last.Revealed)
	return last
}

func countVoted(s domain.RoomState) int {
	n := 0
	for _, u := range s.Users {
		if u.Voted {
			n++
		}
	}
	return n
}
