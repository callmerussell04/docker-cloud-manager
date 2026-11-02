// Package config loads Telemetry's bootstrap settings. Runtime settings come from Core.
package config

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// Bootstrap is Telemetry's process-lifetime configuration.
type Bootstrap struct {
	Port          int
	CoreTarget    string
	InternalToken string
	MaxLogStreams int
	MaxTerminals  int
	HTTP          HTTPServer
	GRPC          GRPC
}

// HTTPServer configures Telemetry's internal HTTP server.
type HTTPServer struct {
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// GRPC configures the client connection to Core.
type GRPC struct {
	RequestTimeout    time.Duration
	MinConnectTimeout time.Duration
}

// Load reads Telemetry's settings. Its error names the first missing required setting.
func Load(lookup envconfig.Lookup) (Bootstrap, error) {
	required := envconfig.NewRequired(lookup)
	cfg := Bootstrap{
		Port:          envconfig.Int(lookup, "TELEMETRY_PORT", 8084),
		CoreTarget:    required.Get("CORE_GRPC_TARGET"),
		InternalToken: required.Get("INTERNAL_SERVICE_TOKEN"),
		MaxLogStreams: envconfig.Int(lookup, "TELEMETRY_MAX_GLOBAL_LOG_STREAMS", 100),
		MaxTerminals:  envconfig.Int(lookup, "TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS", 50),
		HTTP: HTTPServer{
			ReadHeaderTimeout: envconfig.DurationSeconds(lookup, "TELEMETRY_READ_HEADER_TIMEOUT_SECONDS", 5*time.Second),
			IdleTimeout:       envconfig.DurationSeconds(lookup, "TELEMETRY_IDLE_TIMEOUT_SECONDS", 120*time.Second),
			ShutdownTimeout:   envconfig.DurationSeconds(lookup, "TELEMETRY_SHUTDOWN_TIMEOUT_SECONDS", 15*time.Second),
			MaxHeaderBytes:    envconfig.Int(lookup, "TELEMETRY_MAX_HEADER_BYTES", 1<<20),
		},
		GRPC: GRPC{
			RequestTimeout:    envconfig.DurationSeconds(lookup, "TELEMETRY_GRPC_REQUEST_TIMEOUT_SECONDS", 10*time.Second),
			MinConnectTimeout: envconfig.DurationSeconds(lookup, "TELEMETRY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS", 5*time.Second),
		},
	}
	if err := required.Err(); err != nil {
		return Bootstrap{}, err
	}
	return cfg, nil
}
