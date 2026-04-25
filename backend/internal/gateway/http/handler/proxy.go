package handler

import (
	"context"
	"errors"
	"net/http/httputil"
	"net/url"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type BuildOwnerService interface {
	GetUserBuilds(ctx context.Context, ownerID string) ([]dto.BuildDTO, error)
}

func NewBuilderProxyHandler(targetURL string, internalToken string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		c.Request.Header.Del("X-User-Id")
		c.Request.Header.Set("X-User-Id", userID)
		c.Request.Header.Set(internalauth.HeaderName, internalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewAuthorizedBuilderLogsProxy(targetURL string, buildService BuildOwnerService, internalToken string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		buildID := c.Param("id")

		builds, err := buildService.GetUserBuilds(c.Request.Context(), userID)
		if err != nil {
			apperrors.Respond(c, 500, apperrors.ErrInternal)
			return
		}

		allowed := false
		for _, build := range builds {
			if build.ID == buildID {
				allowed = true
				break
			}
		}
		if !allowed {
			apperrors.Respond(c, 404, errors.New("build not found"))
			return
		}

		c.Request.Header.Del("X-User-Id")
		c.Request.Header.Set("X-User-Id", userID)
		c.Request.Header.Set(internalauth.HeaderName, internalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewCoreProxyHandler(targetURL string, internalToken string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		c.Request.Header.Del("X-User-Id")
		c.Request.Header.Set("X-User-Id", userID)
		c.Request.Header.Set(internalauth.HeaderName, internalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}
