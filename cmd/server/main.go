package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"GoScrumPoker/internal/auth"
	"GoScrumPoker/internal/config"
	"GoScrumPoker/internal/dbmigrate"
	"GoScrumPoker/internal/delivery/httpserver"
	"GoScrumPoker/internal/delivery/ws"
	"GoScrumPoker/internal/logging"
	"GoScrumPoker/internal/repository"
	"GoScrumPoker/internal/repository/postgres"
	"GoScrumPoker/internal/service"
)

func main() {
	runMigrations := flag.Bool("migrate", false, "run SQL migrations on startup (requires DATABASE_URL)")
	flag.Parse()

	cfg := config.Load()
	logger := logging.NewLogger(cfg.IsProd())
	if config.IsRender() && cfg.DatabaseURL == "" && cfg.RedisURL == "" {
		logger.Warn().Msg("using in-memory room storage on Render: data is lost on restart; link a PostgreSQL instance and set DATABASE_URL")
	}

	var (
		roomRepo    repository.RoomRepository
		voteRepo    repository.VoteRepository
		poolCleanup func()
		dbBackend   string
		dbPing      func(context.Context) error
	)
	poolCleanup = func() {}

	if cfg.DatabaseURL != "" {
		if cfg.RunMigrationsOnStartup || *runMigrations {
			logger.Info().Str("migrations_path", cfg.MigrationsPath).Msg("running database migrations")
			if err := dbmigrate.Up(cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
				logger.Fatal().Err(err).Msg("database migrations failed")
			}
		}

		pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := postgres.OpenPool(pingCtx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			logger.Fatal().Err(err).Msg("postgres pool failed")
		}
		poolCleanup = func() { pool.Close() }
		pingCtx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		if err := pool.Ping(pingCtx2); err != nil {
			cancel2()
			logger.Fatal().Err(err).Msg("postgres ping failed")
			pool.Close()
			os.Exit(1)
		}
		cancel2()
		roomRepo = postgres.NewPostgresRoomRepository(pool)
		voteRepo = postgres.NewPostgresVoteRepository(pool)
		dbBackend = "postgres"
		dbPing = pool.Ping
		logger.Info().Str("backend", "postgres").Msg("room storage configured")
	} else if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Fatal().Err(err).Msg("invalid REDIS_URL")
		}
		rdb := redis.NewClient(opt)
		pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = rdb.Ping(pingCtx).Err()
		cancel()
		if err != nil {
			logger.Fatal().Err(err).Str("redis_addr", opt.Addr).Msg("redis ping failed")
		}
		redisStore := repository.NewRedis(rdb, cfg.RedisRoomTTL)
		roomRepo = redisStore
		voteRepo = redisStore
		dbBackend = "redis"
		dbPing = func(ctx context.Context) error { return rdb.Ping(ctx).Err() }
		logger.Info().
			Str("backend", "redis").
			Str("redis_addr", opt.Addr).
			Str("room_ttl", cfg.RedisRoomTTL.String()).
			Msg("room storage configured")
	} else {
		mem := repository.NewMemory()
		roomRepo = mem
		voteRepo = mem
		dbBackend = "memory"
		dbPing = nil
		logger.Info().
			Str("backend", "memory").
			Str("hint", "set DATABASE_URL or REDIS_URL for persistence").
			Msg("room storage configured")
	}
	defer poolCleanup()
	defer func() { _ = roomRepo.Close() }()

	roomSvc := service.NewRoomService(roomRepo)
	voteSvc := service.NewVoteService(voteRepo)
	hub := ws.NewHub(roomSvc, voteSvc, logger)
	go hub.Run()

	profileStore := auth.NewProfileStore()
	authSvc := auth.NewService(
		logger,
		profileStore,
		cfg.Auth.GoogleClientID,
		cfg.Auth.GoogleClientSecret,
		cfg.Auth.OAuthRedirectURL,
		cfg.Auth.JWTSecret,
		cfg.Auth.JWTExpires,
		cfg.Auth.PostLoginRedirect,
		cfg.Auth.CookieSecure,
	)
	if authSvc == nil {
		logger.Warn().Msg("Google OAuth disabled: set GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET, OAUTH_REDIRECT_URL, JWT_SECRET")
	}

	srv := &http.Server{
		Addr: cfg.ListenAddr(),
		Handler: httpserver.NewRouter(httpserver.Dependencies{
			Log:                    logger,
			Rooms:                  roomSvc,
			Votes:                  voteSvc,
			Hub:                    hub,
			Auth:                   authSvc,
			DBBackend:              dbBackend,
			DBPing:                 dbPing,
			HealthExposeErrorDetail: cfg.ExposeHealthErrorDetail(),
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info().
			Str("addr", cfg.ListenAddr()).
			Str("port", cfg.Port).
			Str("env", cfg.Env).
			Bool("render", config.IsRender()).
			Msg("server started")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal().Err(err).Msg("http server stopped unexpectedly")
		}
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown
	signal.Stop(shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	logger.Info().
		Int("timeout_sec", int(cfg.ShutdownTimeout/time.Second)).
		Msg("graceful shutdown started")

	hub.Shutdown(ctx)

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			logger.Warn().Err(err).Msg("http server shutdown timed out")
		} else {
			logger.Error().Err(err).Msg("http server shutdown")
		}
		_ = srv.Close()
	}

	logger.Info().Msg("graceful shutdown complete")
}
