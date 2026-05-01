package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (model.Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (model.Tokens, error)
}

type AuthHandler struct {
	service      AuthService
	cookieConfig CookieConfig
}

type CookieConfig struct {
	Name     string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HTTPOnly bool
	SameSite http.SameSite
}

func NewAuthHandler(service AuthService, cookieConfig CookieConfig) *AuthHandler {
	if cookieConfig.Name == "" {
		cookieConfig.Name = "refresh_token"
	}
	if cookieConfig.Path == "" {
		cookieConfig.Path = "/"
	}
	if cookieConfig.MaxAge <= 0 {
		cookieConfig.MaxAge = 30 * 24 * 3600
	}
	if cookieConfig.SameSite == 0 {
		cookieConfig.SameSite = http.SameSiteLaxMode
	}
	return &AuthHandler{
		service:      service,
		cookieConfig: cookieConfig,
	}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	userID, err := h.service.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user_id": userID})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	tokens, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(h.cookieConfig.Name)
	if err != nil {
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tokens, err := h.service.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	h.clearRefreshTokenCookie(c)
	c.Status(http.StatusOK)
}

func (h *AuthHandler) handleAuthError(c *gin.Context, err error) {
	httpresponse.Respond(c, httpresponse.Status(err), err)
}

func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, token string) {
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, token, cfg.MaxAge, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
}

func (h *AuthHandler) clearRefreshTokenCookie(c *gin.Context) {
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, "", -1, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
}
