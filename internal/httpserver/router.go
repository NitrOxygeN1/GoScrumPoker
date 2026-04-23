package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"

	"GoScrumPoker/internal/auth"
	"GoScrumPoker/internal/store"
	appws "GoScrumPoker/internal/ws"
)

// Dependencies bundles shared application services for HTTP handlers.
type Dependencies struct {
	Log   *slog.Logger
	Store store.RoomStore
	Hub   *appws.Hub
	Auth  *auth.Service
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Production services should validate the Origin header against an allowlist.
		_ = r
		return true
	},
}

// NewRouter wires HTTP routes and middleware.
func NewRouter(dep Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(dep.Log))

	if dep.Auth != nil {
		dep.Auth.Register(r)
	}

	r.Post("/rooms", createRoom(dep))
	r.Get("/rooms/{id}", getRoom(dep))
	r.Get("/ws/{roomId}", serveWS(dep))

	return r
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func createRoom(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		room, err := dep.Store.CreateRoom(r.Context())
		if err != nil {
			dep.Log.Error("create room failed", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "could not create room")
			return
		}
		dep.Log.Info("room created", "room_id", room.ID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": room.ID})
	}
}

func getRoom(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		snap, ok, err := dep.Store.Snapshot(r.Context(), id)
		if err != nil {
			dep.Log.Error("room snapshot failed", "room_id", id, "err", err)
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
		roomID := chi.URLParam(r, "roomId")
		if strings.TrimSpace(roomID) == "" {
			writeJSONError(w, http.StatusBadRequest, "missing room id")
			return
		}
		ok, err := dep.Store.Exists(r.Context(), roomID)
		if err != nil {
			dep.Log.Error("room exists check failed", "room_id", roomID, "err", err)
			writeJSONError(w, http.StatusInternalServerError, "storage error")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "room not found")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			dep.Log.Info("websocket upgrade failed", "err", err)
			return
		}

		client := appws.NewClient(dep.Hub, roomID, conn, dep.Log)
		go client.Run()
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
