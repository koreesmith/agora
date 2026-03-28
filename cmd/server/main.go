package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/agora-social/agora/internal/admin"
	"github.com/agora-social/agora/internal/albums"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/blocks"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/dm"
	"github.com/agora-social/agora/internal/feed"
	"github.com/agora-social/agora/internal/federation"
	"github.com/agora-social/agora/internal/friends"
	"github.com/agora-social/agora/internal/groups"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/moderation"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/search"
	"github.com/agora-social/agora/internal/store"
	"github.com/agora-social/agora/internal/users"
)

func main() {
	cfg := config.Load()
	log.Printf("agora: starting on %s (env: %s)", cfg.HTTPAddr, cfg.Environment)

	// ── Database ──────────────────────────────────────────────────────────
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// ── Services ──────────────────────────────────────────────────────────
	emailSvc  := notifications.NewEmailService(db, cfg)
	notifSvc  := notifications.NewService(db, emailSvc)
	mediaSvc  := media.NewService(cfg.UploadDir)
	userSvc   := users.NewService(db, mediaSvc)
	authSvc   := auth.NewService(db, cfg, notifSvc)
	friendSvc := friends.NewService(db, notifSvc)
	feedSvc   := feed.NewService(db, notifSvc, mediaSvc)
	groupSvc  := groups.NewService(db, notifSvc)
	albumsSvc := albums.NewService(db, mediaSvc)
	feedSvc.SetAlbums(albumsSvc)
	searchSvc := search.NewService(db)
	modSvc    := moderation.NewService(db, notifSvc)
	adminSvc  := admin.NewService(db, cfg, notifSvc)
	fedSvc    := federation.NewService(db, cfg, feedSvc, userSvc)
	dmSvc     := dm.New(db)
	blocksSvc := blocks.New(db)

	// Wire federation into services that need to broadcast activities
	friendSvc.SetFed(fedSvc)
	feedSvc.SetFed(fedSvc)
	userSvc.SetFed(fedSvc)

	// ── Router ────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		timeout := middleware.Timeout(60 * time.Second)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout for WebSocket connections
			if r.Header.Get("Upgrade") == "websocket" {
				next.ServeHTTP(w, r)
				return
			}
			timeout(next).ServeHTTP(w, r)
		})
	})
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Static uploads
	r.Mount("/uploads", mediaSvc.FileServer())

	// ── API routes ────────────────────────────────────────────────────────
	r.Route("/api", func(r chi.Router) {
		// Public (includes /setup, /instance, and /auth/*)
		auth.RegisterPublicRoutes(r, authSvc)
		auth.RegisterInstanceRoute(r, authSvc)
		// Public one-click unsubscribe (no auth required — linked from emails)
		r.Post("/notifications/unsubscribe", notifSvc.OneClickUnsubscribe)
		r.Get("/notifications/unsubscribe",  notifSvc.UnsubscribePage)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware)
			r.Post("/auth/change-password", authSvc.ChangePassword)
			r.Post("/invites/send",         authSvc.SendUserInvite)
			users.RegisterRoutes(r, userSvc)
			friends.RegisterRoutes(r, friendSvc)
			feed.RegisterRoutes(r, feedSvc)
			groups.RegisterRoutes(r, groupSvc)
			notifications.RegisterRoutes(r, notifSvc)
			search.RegisterRoutes(r, searchSvc)
			moderation.RegisterRoutes(r, modSvc)
			media.RegisterRoutes(r, mediaSvc)
			albums.RegisterRoutes(r, albumsSvc)
			dm.RegisterRoutes(r, dmSvc)
			blocks.RegisterRoutes(r, blocksSvc)
		})

		// Admin only
		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware)
			r.Use(authSvc.RequireAdmin)
			admin.RegisterRoutes(r, adminSvc)
		})
	})

	// Federation endpoints
	federation.RegisterRoutes(r, fedSvc)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// ── Background jobs ───────────────────────────────────────────────────
	go fedSvc.StartBackgroundSync(context.Background())
	go userSvc.StartDeletionCleanup(context.Background())

	// ── HTTP server with graceful shutdown ────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("agora: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-done
	log.Println("agora: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("agora: stopped")
}
