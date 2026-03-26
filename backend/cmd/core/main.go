package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/app"
	_ "github.com/lib/pq"
)

func main() {
	portStr := os.Getenv("CORE_GRPC_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 50052
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		log.Fatal("BASE_DOMAIN environment variable is not set")
	}

	application, err := app.New(port, dbURL, baseDomain)
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
