package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
)

func validateTelemetryLogQuery(c *gin.Context) bool {
	if raw := c.Query("tail"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return false
		}
	}
	if !validateBoolQuery(c, "follow") || !validateBoolQuery(c, "timestamps") {
		return false
	}
	if raw := c.Query("since"); raw != "" {
		if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return true
		}
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return false
		}
	}
	return true
}

func validateTelemetryTerminalQuery(c *gin.Context) bool {
	if c.Query("cmd") == "" && len(c.QueryArray("arg")) > 0 {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return false
	}
	for _, key := range []string{"rows", "cols"} {
		if raw := c.Query(key); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 32)
			if err != nil || value == 0 {
				httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
				return false
			}
		}
	}
	for _, value := range append([]string{c.Query("cmd")}, c.QueryArray("arg")...) {
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return false
		}
	}
	return true
}

func validateBoolQuery(c *gin.Context, key string) bool {
	if raw := c.Query(key); raw != "" {
		if _, err := strconv.ParseBool(raw); err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return false
		}
	}
	return true
}

func validateWebSocketOrigin(c *gin.Context, allowedOrigins []string) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range allowedOrigins {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	httpresponse.Respond(c, http.StatusForbidden, apperrors.ErrForbidden)
	return false
}
