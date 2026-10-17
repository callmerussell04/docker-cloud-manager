package model

import "time"

type Tokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
}
