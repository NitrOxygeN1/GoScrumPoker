package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"GoScrumPoker/internal/auth"
	"GoScrumPoker/internal/delivery/ws"
	"GoScrumPoker/internal/logging"
	"GoScrumPoker/internal/service"
)

// Dependencies bundles constructor-injected collaborators for HTTP delivery.
type Dependencies struct {
	Log       zerolog.Logger
	Rooms     *service.RoomService
	Votes     *service.VoteService
	Hub       *ws.Hub
	Auth      *auth.Service
	DBBackend string
	// DBPing checks connectivity to the primary datastore (Postgres or Redis). Nil for in-memory.
	DBPing func(context.Context) error
	// HealthExposeErrorDetail includes raw DB error strings in /health JSON. Disable on production/Render.
	HealthExposeErrorDetail bool
	// WebRoot, if set, is the on-disk Vite `dist` folder (index + assets) for the SPA. Empty = API + WS only.
	WebRoot string
	// CSPFrameAncestorsExtra adds space-separated frame-ancestors sources (optional).
	CSPFrameAncestorsExtra string
	// MeetCloudProjectNumber is injected into index.html as
	// <meta name="gsp-cloud-project-number"> so the SPA can initialize the Meet Web
	// Add-ons SDK. Required for Meet's host shell to consider the add-on launched.
	MeetCloudProjectNumber string
}

// WebSocket upgrades run through meetEmbedMiddleware; the iframe document origin is the
// app host (same as window.location.host), so permissive CheckOrigin keeps Meet embeds working.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		_ = r
		return true
	},
}

// NewRouter wires HTTP routes and middleware.
func NewRouter(dep Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(meetEmbedMiddleware(dep.CSPFrameAncestorsExtra))
	r.Use(normalizePathMiddleware())
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(recoverer(dep.Log))
	r.Use(requestLogger(dep.Log))

	if dep.Auth != nil {
		dep.Auth.Register(r)
	}

	// Do not register r.Head("/") or r.Get("/") alone: chi would bind "/" for that method only
	// and other methods (e.g. GET) would 405, breaking the SPA home page. Uptime/monitoring
	// can use GET/HEAD on "/" via NotFound -> static (ServeContent supports both).

	r.Get("/health", health(dep))
	r.Post("/rooms", createRoom(dep))
	r.Get("/rooms/{id}", getRoom(dep))
	r.Get("/ws/{roomId}", serveWS(dep))

	if dep.WebRoot != "" {
		staticH := staticFileHandler(dep.WebRoot, dep.MeetCloudProjectNumber)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			staticH.ServeHTTP(w, r)
		})
	}

	return r
}

func recoverer(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					lg := logging.LoggerFromRequest(log, r)
					lg.Error().Interface("panic", rec).Bytes("stack", debug.Stack()).Msg("panic recovered")
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			lg := logging.LoggerFromRequest(log, r)
			lg.Info().
				Str("http_event", "request_complete").
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Int("bytes", ww.BytesWritten()).
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Msg("http request")
		})
	}
}

func createRoom(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lg := logging.LoggerFromRequest(dep.Log, r)
		room, err := dep.Rooms.CreateRoom(r.Context())
		if err != nil {
			lg.Error().Err(err).Msg("create room failed")
			writeJSONError(w, http.StatusInternalServerError, "could not create room")
			return
		}
		lg.Info().Str("room_id", room.ID).Msg("room created")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": room.ID})
	}
}

func getRoom(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lg := logging.LoggerFromRequest(dep.Log, r)
		id := chi.URLParam(r, "id")
		snap, ok, err := dep.Rooms.GetSnapshot(r.Context(), id)
		if err != nil {
			lg.Error().Err(err).Str("room_id", id).Msg("room snapshot failed")
			writeJSONError(w, http.StatusInternalServerError, "storage error")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "room not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
	}
}

func serveWS(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lg := logging.LoggerFromRequest(dep.Log, r)
		roomID := chi.URLParam(r, "roomId")
		if strings.TrimSpace(roomID) == "" {
			writeJSONError(w, http.StatusBadRequest, "missing room id")
			return
		}
		lg = lg.With().Str("room_id", roomID).Logger()

		ok, err := dep.Rooms.RoomExists(r.Context(), roomID)
		if err != nil {
			lg.Error().Err(err).Msg("room exists check failed")
			writeJSONError(w, http.StatusInternalServerError, "storage error")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "room not found")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			lg.Info().Err(err).Msg("websocket upgrade failed")
			return
		}

		lg.Info().Msg("websocket connected")
		client := ws.NewClient(dep.Hub, roomID, conn, lg)
		dep.Hub.Track(client)
		defer dep.Hub.Untrack(client)
		defer lg.Info().Msg("websocket session ended")
		client.Run()
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
