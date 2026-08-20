package dto

type RegisterRequest struct {
	Username string
	Email    string
	Password string
}

type LoginRequest struct {
	Username string
	Password string
}

type RefreshRequest struct {
	RefreshToken string
}

type VerifyTokenRequest struct {
	AccessToken string
}

type CheckPermissionRequest struct {
	AccessToken string
	Permission  string
}
