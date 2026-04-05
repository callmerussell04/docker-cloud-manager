package main

import (
	"log"
	"os"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/app"
)

func main() {
	portStr := os.Getenv("BUILDER_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 8081
	}

	storagePath := os.Getenv("BUILD_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "/tmp/builds"
	}

	application, err := app.New(port, storagePath)
	if err != nil {
		log.Fatalf("failed to initialize builder app: %v", err)
	}

	log.Printf("Builder service starting on port %d", port)
	if err := application.Run(); err != nil {
		log.Fatalf("builder server failed: %v", err)
	}
}
