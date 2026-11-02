package model

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/platform/accessscope"
)

const (
	TelemetryStreamLogs     = "logs"
	TelemetryStreamTerminal = "terminal"
)

type TelemetryTicketClaims struct {
	ContainerID string
	StreamType  string
	AdminRoute  bool
	Scope       accessscope.Scope
	ExpiresAt   time.Time
}

type TelemetryTicket struct {
	Ticket    string
	ExpiresAt time.Time
}
