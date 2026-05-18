package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	apihttp "GoScrumPoker/internal/delivery/httpserver"
	"GoScrumPoker/internal/delivery/ws"
	"GoScrumPoker/internal/repository"
	"GoScrumPoker/internal/service"
)

// newMeetTestServer mirrors newTestServer in integration_test.go; kept separate
// here so the meet-room suite stays independent of the WebSocket-flow tests.
func newMeetTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
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
	return srv, func() {
		srv.Close()
		_ = mem.Close()
	}
}

func TestRoomForMeet_returnsSameRoomForSameMeetingID(t *testing.T) {
	srv, cleanup := newMeetTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first := postRoomByMeet(t, ctx, srv.URL, "meeting-AAA")
	if !first.Created {
		t.Fatalf("first call should report created=true, got %+v", first)
	}
	if first.ID == "" {
		t.Fatal("first call returned empty room id")
	}

	second := postRoomByMeet(t, ctx, srv.URL, "meeting-AAA")
	if second.Created {
		t.Fatalf("second call should report created=false (idempotent), got %+v", second)
	}
	if second.ID != first.ID {
		t.Fatalf("second call room id %q != first %q (must be idempotent)", second.ID, first.ID)
	}

	other := postRoomByMeet(t, ctx, srv.URL, "meeting-BBB")
	if other.ID == first.ID {
		t.Fatalf("distinct meeting-id must yield a distinct room (%q == %q)", other.ID, first.ID)
	}
	if !other.Created {
		t.Fatal("new meeting-id should report created=true")
	}
}

func TestRoomForMeet_acceptsMeetingCodeFallback(t *testing.T) {
	srv, cleanup := newMeetTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Omit meeting_id; server should fall back to meeting_code as the binding key.
	resp := postMeetBody(t, ctx, srv.URL, `{"meeting_code":"abc-defg-hij"}`)
	first := decodeMeetResponse(t, resp)
	if first.ID == "" {
		t.Fatal("empty id")
	}

	again := decodeMeetResponse(t, postMeetBody(t, ctx, srv.URL,
		`{"meeting_code":"abc-defg-hij"}`))
	if again.ID != first.ID {
		t.Fatalf("meeting_code fallback should be idempotent: %q != %q", again.ID, first.ID)
	}
}

func TestRoomForMeet_rejectsEmptyBody(t *testing.T) {
	srv, cleanup := newMeetTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp := postMeetBody(t, ctx, srv.URL, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

type meetRoomResponse struct {
	ID      string `json:"id"`
	Created bool   `json:"created"`
}

func postRoomByMeet(t *testing.T, ctx context.Context, baseURL, meetingID string) meetRoomResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"meeting_id": meetingID})
	return decodeMeetResponse(t, postMeetBody(t, ctx, baseURL, string(body)))
}

func postMeetBody(t *testing.T, ctx context.Context, baseURL, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/rooms/by-meet", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeMeetResponse(t *testing.T, resp *http.Response) meetRoomResponse {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(raw))
	}
	var out meetRoomResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
