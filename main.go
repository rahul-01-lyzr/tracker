package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"lyzr-tracker/config"
	"lyzr-tracker/handlers"
	"lyzr-tracker/middleware"
	"lyzr-tracker/pipeline"
	"lyzr-tracker/tracker"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("INFO: no .env file found, using system environment variables")
	}

	// Load Configuration 
	cfg := config.Load()

	// Initialize Mixpanel Tracker 
	mp := tracker.New(cfg.MixpanelToken)

	// Initialize Pipeline 
	pipe := pipeline.New(
		mp,
		cfg.ChannelBufferSize,
		cfg.BatchSize,
		cfg.WorkerCount,
		cfg.FlushInterval,
	)
	pipe.Start()
	
	// Initialize Fiber 
	app := fiber.New(fiber.Config{
		Prefork:               false,
		DisableStartupMessage: false,
		BodyLimit:             4 * 1024 * 1024, // 4 MB
		ReadBufferSize:        8192,
		WriteBufferSize:       4096,
		ReduceMemoryUsage:     false,           // keep buffers hot
	})

	// Middleware 
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,OPTIONS",
		AllowHeaders: "Authorization,Content-Type",
	}))
	app.Use(recover.New()) // panic recovery

	// Routes 
	// Health 
	app.Get("/health", handlers.HealthHandler())

	// Authenticated event routes
	api := app.Group("/", middleware.Auth(cfg.AuthToken))
	api.Post("/track", handlers.TrackHandler(pipe))
	api.Post("/track/batch", handlers.BatchTrackHandler(pipe))
	api.Post("/profile", handlers.ProfileHandler(mp))
	api.Get("/stats", handlers.StatsHandler(pipe))

	// Graceful Shutdown 
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("INFO: shutdown signal received")
		pipe.Shutdown()     // drain pipeline
		_ = app.Shutdown()  // stop accepting new connections
	}()

	// Start Server 
	log.Printf("INFO: lyzr-tracker starting on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("FATAL: server error: %v", err)
	}
}
