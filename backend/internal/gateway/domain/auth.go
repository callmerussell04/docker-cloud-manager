package domain

type Tokens struct {
	AccessToken  string
	RefreshToken string
}

type AuthUser struct {
	UserID   string
	Username string
	Role     string
}
