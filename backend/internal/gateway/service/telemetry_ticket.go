package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type TelemetryTicketStore struct {
	mu      sync.Mutex
	tickets map[string]model.TelemetryTicketClaims
	ttl     time.Duration
	now     func() time.Time
}

func NewTelemetryTicketStore(ttl time.Duration) *TelemetryTicketStore {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &TelemetryTicketStore{
		tickets: make(map[string]model.TelemetryTicketClaims),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (s *TelemetryTicketStore) Issue(ctx context.Context, claims model.TelemetryTicketClaims) (model.TelemetryTicket, error) {
	select {
	case <-ctx.Done():
		return model.TelemetryTicket{}, ctx.Err()
	default:
	}

	token, err := randomTicketToken()
	if err != nil {
		return model.TelemetryTicket{}, err
	}
	now := s.now()
	claims.ExpiresAt = now.Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	s.tickets[token] = claims
	return model.TelemetryTicket{Ticket: token, ExpiresAt: claims.ExpiresAt}, nil
}

func (s *TelemetryTicketStore) Consume(ctx context.Context, token, expectedContainerID, expectedStreamType string, expectedAdminRoute bool) (model.TelemetryTicketClaims, error) {
	select {
	case <-ctx.Done():
		return model.TelemetryTicketClaims{}, ctx.Err()
	default:
	}
	if !validTicketToken(token) {
		return model.TelemetryTicketClaims{}, apperrors.ErrUnauthorized
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	claims, ok := s.tickets[token]
	if ok {
		delete(s.tickets, token)
	}
	s.cleanupLocked(now)
	if !ok {
		return model.TelemetryTicketClaims{}, apperrors.ErrUnauthorized
	}
	if !claims.ExpiresAt.After(now) {
		return model.TelemetryTicketClaims{}, apperrors.ErrUnauthorized
	}
	if claims.ContainerID != expectedContainerID || claims.StreamType != expectedStreamType || claims.AdminRoute != expectedAdminRoute {
		return model.TelemetryTicketClaims{}, apperrors.ErrForbidden
	}
	return claims, nil
}

func (s *TelemetryTicketStore) cleanupLocked(now time.Time) {
	for token, claims := range s.tickets {
		if !claims.ExpiresAt.After(now) {
			delete(s.tickets, token)
		}
	}
}

func randomTicketToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", errors.Join(apperrors.ErrInternal, err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func validTicketToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}
