package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/app"
	_ "github.com/lib/pq"
)

func main() {
	portStr := os.Getenv("SSO_GRPC_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 50051
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is not set")
	}

	accessTTL := 15 * time.Minute
	refreshTTL := 30 * 24 * time.Hour

	application, err := app.New(port, dbURL, jwtSecret, accessTTL, refreshTTL)
	if err != nil {
		log.Fatalf("failed to initialize application: %v", err)
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
