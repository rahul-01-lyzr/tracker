package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	Port string

	// Auth
	AuthToken string

	// Mixpanel
	MixpanelToken string

	// Pipeline tuning
	ChannelBufferSize int
	BatchSize         int
	FlushInterval     time.Duration
	WorkerCount       int
}

// Load reads configuration from environment variables.
func Load() *Config {
	cfg := &Config{
		Port:              getEnv("PORT"),
		AuthToken:         getEnv("AUTH_TOKEN"),
		MixpanelToken:     getEnv("MIXPANEL_TOKEN"),
		ChannelBufferSize: getEnvInt("CHANNEL_BUFFER_SIZE"),
		BatchSize:         getEnvInt("BATCH_SIZE"),
		FlushInterval:     getEnvDuration("FLUSH_INTERVAL"),
		WorkerCount:       getEnvInt("WORKER_COUNT"),
	}

	return cfg
}

// getEnv fetches a required env variable
func getEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("FATAL: required environment variable %s is not set", key)
	}
	return trimComment(v)
}

// trimComment removes inline comments (# ...)
func trimComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func getEnvInt(key string) int {
	v := getEnv(key)

	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("FATAL: invalid int for %s=%s", key, v)
	}

	return n
}

func getEnvDuration(key string) time.Duration {
	v := getEnv(key)

	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("FATAL: invalid duration for %s=%s", key, v)
	}

	return d
}