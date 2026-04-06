package handler

import (
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

func NewBuilderProxyHandler(targetURL string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		c.Request.Header.Set("X-User-Id", userID)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewCoreProxyHandler(targetURL string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		c.Request.Header.Set("X-User-Id", userID)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}
