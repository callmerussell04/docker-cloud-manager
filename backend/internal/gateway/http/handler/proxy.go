package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

type BuildOwnerService interface {
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
}

type ProxyOptions struct {
	TargetURL      string
	InternalToken  string
	Transport      http.RoundTripper
	AllowedOrigins []string
}

func NewBuilderProxyHandler(opts ProxyOptions) (gin.HandlerFunc, error) {
	proxy, err := newGatewayProxy(opts)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		prepareProxyRequest(c, opts.InternalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewAuthorizedBuildProxyHandler(opts ProxyOptions, buildService BuildOwnerService) (gin.HandlerFunc, error) {
	proxy, err := newGatewayProxy(opts)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		buildID, ok := pathUUID(c, "id")
		if !ok {
			return
		}

		build, err := buildService.GetBuild(c.Request.Context(), buildID)
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		if build.ID == "" {
			httpresponse.Respond(c, httpresponse.Status(apperrors.ErrNotFound), apperrors.ErrNotFound)
			return
		}

		prepareProxyRequest(c, opts.InternalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewCoreProxyHandler(opts ProxyOptions) (gin.HandlerFunc, error) {
	proxy, err := newGatewayProxy(opts)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		prepareProxyRequest(c, opts.InternalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewTelemetryLogsProxyHandler(opts ProxyOptions) (gin.HandlerFunc, error) {
	proxy, err := newGatewayProxy(opts)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		if _, ok := pathUUID(c, "id"); !ok {
			return
		}
		if !validateTelemetryLogQuery(c) {
			return
		}
		clearWriteDeadline(c)
		prepareProxyRequest(c, opts.InternalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func NewTelemetryTerminalProxyHandler(opts ProxyOptions) (gin.HandlerFunc, error) {
	proxy, err := newGatewayProxy(opts)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		if _, ok := pathUUID(c, "id"); !ok {
			return
		}
		if !validateWebSocketOrigin(c, opts.AllowedOrigins) || !validateTelemetryTerminalQuery(c) {
			return
		}
		clearWriteDeadline(c)
		prepareProxyRequest(c, opts.InternalToken)
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func newGatewayProxy(opts ProxyOptions) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(opts.TargetURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	if opts.Transport != nil {
		proxy.Transport = opts.Transport
	}
	proxy.ErrorHandler = proxyErrorHandler
	return proxy, nil
}

func prepareProxyRequest(c *gin.Context, internalToken string) {
	clearProxyIdentityHeaders(c.Request.Header)
	internalauth.SetScopeHeadersFromContext(c.Request.Header, c.Request.Context())
	c.Request.Header.Set(internalauth.HeaderName, internalToken)
	if requestID := logging.RequestIDFromContext(c.Request.Context()); requestID != "" {
		c.Request.Header.Set(logging.RequestIDHeader, requestID)
	}
}

func clearProxyIdentityHeaders(header http.Header) {
	header.Del("Authorization")
	header.Del("Cookie")
	header.Del(internalauth.HeaderUserID)
	header.Del(internalauth.HeaderUsername)
	header.Del(internalauth.HeaderRole)
	header.Del(internalauth.HeaderScope)
	header.Del(internalauth.HeaderName)
	header.Del("X-Forwarded-User")
	header.Del("X-Forwarded-Email")
	header.Del("X-Forwarded-Groups")
	header.Del("X-Remote-User")
}

func proxyErrorHandler(rw http.ResponseWriter, req *http.Request, err error) {
	statusCode := http.StatusServiceUnavailable
	appErr := apperrors.ErrUnavailable
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		statusCode = http.StatusGatewayTimeout
		appErr = apperrors.ErrTimeout
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(statusCode)
	_ = json.NewEncoder(rw).Encode(httpresponse.ErrorResponse{
		Error:     apperrors.SafeMessage(appErr),
		RequestID: logging.RequestIDFromContext(req.Context()),
	})
}

func clearWriteDeadline(c *gin.Context) {
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
}

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
