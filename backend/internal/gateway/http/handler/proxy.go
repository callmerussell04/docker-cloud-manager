package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

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
	TargetURL     string
	InternalToken string
	Transport     http.RoundTripper
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
