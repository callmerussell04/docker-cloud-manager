package main

import (
	"log"
	"os"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/app"
)

func main() {
	portStr := os.Getenv("GATEWAY_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 8080
	}

	ssoTarget := os.Getenv("SSO_GRPC_TARGET")
	if ssoTarget == "" {
		log.Fatal("SSO_GRPC_TARGET environment variable is not set")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is not set")
	}

	application, err := app.New(port, ssoTarget, jwtSecret)
	if err != nil {
		log.Fatalf("failed to initialize gateway app: %v", err)
	}

	if err := application.Run(); err != nil {
		log.Fatalf("gateway server failed: %v", err)
	}
}
