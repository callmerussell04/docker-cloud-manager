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
		port = 8081
	}

	ssoTarget := os.Getenv("SSO_GRPC_TARGET")
	if ssoTarget == "" {
		log.Fatal("SSO_GRPC_TARGET environment variable is not set")
	}

	coreTarget := os.Getenv("CORE_GRPC_TARGET")
	if coreTarget == "" {
		log.Fatal("CORE_GRPC_TARGET environment variable is not set")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is not set")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		log.Fatal("INTERNAL_SERVICE_TOKEN environment variable is not set")
	}

	builderHttpTarget := os.Getenv("BUILDER_HTTP_TARGET")
	if builderHttpTarget == "" {
		log.Fatal("BUILDER_HTTP_TARGET environment variable is not set")
	}

	coreHttpTarget := os.Getenv("CORE_HTTP_TARGET")
	if coreHttpTarget == "" {
		log.Fatal("CORE_HTTP_TARGET environment variable is not set")
	}

	application, err := app.New(port, ssoTarget, coreTarget, builderHttpTarget, coreHttpTarget, jwtSecret, internalToken)
	if err != nil {
		log.Fatalf("failed to initialize gateway app: %v", err)
	}

	if err := application.Run(); err != nil {
		log.Fatalf("gateway server failed: %v", err)
	}
}
