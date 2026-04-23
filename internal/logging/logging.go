package logging

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// NewLogger returns a JSON logger in production or a console writer in development.
func NewLogger(isProd bool) zerolog.Logger {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	out := io.Writer(os.Stdout)
	if isProd {
		return zerolog.New(out).Level(zerolog.InfoLevel).With().Timestamp().Str("service", "goscrumppoker").Logger()
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		Level(zerolog.InfoLevel).
		With().Timestamp().Str("service", "goscrumppoker").Logger()
}

// LoggerFromRequest returns a child logger with request_id from chi middleware when present.
func LoggerFromRequest(l zerolog.Logger, r *http.Request) zerolog.Logger {
	rid := middleware.GetReqID(r.Context())
	if rid == "" {
		return l
	}
	return l.With().Str("request_id", rid).Logger()
}
