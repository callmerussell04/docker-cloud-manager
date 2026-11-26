package registry

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Signer struct {
	key         *rsa.PrivateKey
	certificate string
	issuer      string
}

func NewSigner(certificateFile, keyFile, issuer string) (*Signer, error) {
	pair, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load registry signing identity: %w", err)
	}
	key, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 || issuer == "" {
		return nil, errors.New("invalid registry signing identity")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse registry signing certificate: %w", err)
	}
	if time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
		return nil, errors.New("invalid registry signing certificate")
	}
	return &Signer{key: key, certificate: base64.StdEncoding.EncodeToString(pair.Certificate[0]), issuer: issuer}, nil
}

func (s *Signer) SignRegistryToken(ctx context.Context, grant model.RegistryTokenGrant) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	access := make([]map[string]any, 0, len(grant.Access))
	for _, a := range grant.Access {
		access = append(access, map[string]any{"type": "repository", "name": a.Repository, "actions": a.Actions})
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": s.issuer, "sub": grant.Subject, "aud": grant.Service, "iat": grant.IssuedAt.Unix(), "nbf": grant.IssuedAt.Unix(), "exp": grant.ExpiresAt.Unix(), "jti": uuid.NewString(), "access": access,
	})
	token.Header["x5c"] = []string{s.certificate}
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign registry token: %w", err)
	}
	return signed, nil
}
