package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agora-social/agora/internal/admin"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/feed"
	"github.com/agora-social/agora/internal/federation"
	"github.com/agora-social/agora/internal/friends"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/moderation"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/search"
	"github.com/agora-social/agora/internal/users"
	"github.com/agora-social/agora/pkg/config"
	"github.com/agora-social/agora/pkg/database"
	"github.com/agora-social/agora/pkg/middleware"
	rdb "github.com/agora-social/agora/pkg/redis"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	cfg := config.Load()

	// Connect to database
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Connect to Redis
	redisClient, err := rdb.Connect(cfg.RedisURL)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Seed default admin if needed
	if err := database.SeedAdmin(db); err != nil {
		log.Fatalf("Failed to seed admin: %v", err)
	}

	// Initialize services
	emailSvc := notifications.NewEmailService(db)
	notifSvc := notifications.NewService(db, redisClient, emailSvc)
	mediaSvc := media.NewService(cfg.MediaDir)
	userSvc := users.NewService(db, mediaSvc)
	authSvc := auth.NewService(db, cfg, notifSvc)
	friendSvc := friends.NewService(db, notifSvc)
	feedSvc := feed.NewService(db, redisClient, notifSvc, mediaSvc)
	searchSvc := search.NewService(db)
	modSvc := moderation.NewService(db, notifSvc)
	adminSvc := admin.NewService(db, cfg)
	fedSvc := federation.NewService(db, cfg, feedSvc, userSvc)

	// Build router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// JWT middleware
	jwtMiddleware := middleware.NewJWTMiddleware(cfg.JWTSecret)

	// Routes
	r.Route("/api", func(r chi.Router) {
		// Public routes
		r.Group(func(r chi.Router) {
			auth.RegisterRoutes(r, authSvc)
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(jwtMiddleware.Authenticate)
			users.RegisterRoutes(r, userSvc)
			friends.RegisterRoutes(r, friendSvc)
			feed.RegisterRoutes(r, feedSvc)
			notifications.RegisterRoutes(r, notifSvc)
			search.RegisterRoutes(r, searchSvc)
			moderation.RegisterRoutes(r, modSvc)
			media.RegisterRoutes(r, mediaSvc)
		})

		// Admin routes
		r.Group(func(r chi.Router) {
			r.Use(jwtMiddleware.Authenticate)
			r.Use(jwtMiddleware.RequireAdmin)
			admin.RegisterRoutes(r, adminSvc)
		})
	})

	// Federation routes (public with signature verification)
	federation.RegisterRoutes(r, fedSvc)

	// Start federation background jobs
	go fedSvc.StartBackgroundSync(context.Background())

	// Start account deletion cleanup job
	go userSvc.StartDeletionCleanup(context.Background())

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Agora backend starting on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited cleanly")
}
