package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"GoScrumPoker/internal/logging"
)

const healthPingTimeout = 2 * time.Second

type healthResponse struct {
	Status   string     `json:"status"`
	Database dbHealth   `json:"database"`
}

type dbHealth struct {
	Status  string `json:"status"`
	Backend string `json:"backend"`
	Error   string `json:"error,omitempty"`
}

func health(dep Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lg := logging.LoggerFromRequest(dep.Log, r)

		resp := healthResponse{
			Status: "OK",
			Database: dbHealth{
				Status:  "ok",
				Backend: dep.DBBackend,
			},
		}
		code := http.StatusOK

		if dep.DBPing != nil {
			ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
			err := dep.DBPing(ctx)
			cancel()
			if err != nil {
				resp.Status = "ERROR"
				resp.Database.Status = "error"
				if dep.HealthExposeErrorDetail {
					resp.Database.Error = err.Error()
				} else {
					resp.Database.Error = "unavailable"
				}
				code = http.StatusServiceUnavailable
				lg.Error().Err(err).Str("backend", dep.DBBackend).Msg("health database ping failed")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			lg.Error().Err(err).Msg("health response encode failed")
		}
	}
}
