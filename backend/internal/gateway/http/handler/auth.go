package handler

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (domain.Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (domain.Tokens, error)
}

type AuthHandler struct {
	service AuthService
}

func NewAuthHandler(service AuthService) *AuthHandler {
	return &AuthHandler{
		service: service,
	}
}

type registerRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	userID, err := h.service.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			apperrors.Respond(c, http.StatusConflict, apperrors.ErrAlreadyExists)
			return
		}
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user_id": userID})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	tokens, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidCredentials) {
			apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrInvalidCredentials)
			return
		}
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tokens, err := h.service.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) {
			apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrInvalidToken)
			return
		}
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("refresh_token", "", -1, "/", "", false, true)
	c.Status(http.StatusOK)
}

func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, token string) {
	maxAge := 30 * 24 * 3600
	secure := os.Getenv("COOKIE_SECURE") == "true"
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("refresh_token", token, maxAge, "/", "", secure, true)
}
