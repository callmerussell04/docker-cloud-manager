package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	_ "github.com/lib/pq"
)

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if value, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return fallback
}

func main() {
	port := getEnvInt("CORE_GRPC_PORT", 50052)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		log.Fatal("BASE_DOMAIN environment variable is not set")
	}

	cfg := service.ContainerConfig{
		MaxContainersPerUser:  getEnvInt("MAX_CONTAINERS_PER_USER", 5),
		ReservedSystemMemory:  getEnvInt64("RESERVED_SYSTEM_MEMORY_BYTES", 2*1024*1024*1024),
		OvercommitFactor:      getEnvFloat("OVERCOMMIT_FACTOR", 1.5),
		BaseMemoryReservation: getEnvInt64("BASE_MEMORY_RESERVATION_BYTES", 256*1024*1024),
		MaxBurstMemoryLimit:   getEnvInt64("MAX_BURST_MEMORY_LIMIT_BYTES", 2*1024*1024*1024),
		BaseDomain:            baseDomain,
	}

	application, err := app.New(port, dbURL, cfg)
	if err != nil {
		log.Fatalf("failed to initialize core application: %v", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			log.Fatalf("grpc server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	application.Stop()
}
