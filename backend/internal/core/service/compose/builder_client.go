package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type HTTPBuilderClient struct {
	baseURL       string
	internalToken string
	cfg           ConfigProvider
	httpClient    *http.Client
}

func NewHTTPBuilderClient(baseURL, internalToken string, cfg ConfigProvider, httpClient *http.Client) *HTTPBuilderClient {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &HTTPBuilderClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		internalToken: internalToken,
		cfg:           cfg,
		httpClient:    httpClient,
	}
}

func (c *HTTPBuilderClient) TriggerBuild(ctx context.Context, srv model.ComposeService, archiveBytes []byte) (uuid.UUID, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("tag", srv.ImageTag)
	_ = writer.WriteField("context", srv.BuildContext)
	_ = writer.WriteField("dockerfile", srv.Dockerfile)
	if len(srv.BuildArgs) > 0 {
		argsJSON, _ := json.Marshal(srv.BuildArgs)
		_ = writer.WriteField("build_args", string(argsJSON))
	}

	part, err := writer.CreateFormFile("archive", "compose.zip")
	if err == nil {
		_, _ = part.Write(archiveBytes)
	}
	if err := writer.Close(); err != nil {
		return uuid.Nil, err
	}

	httpCtx, cancel := c.timeoutContext(ctx)
	defer cancel()

	req, err := c.newRequest(httpCtx, http.MethodPost, "/api/v1/images/build", body)
	if err != nil {
		return uuid.Nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return uuid.Nil, builderHTTPError(resp)
	}

	var result struct {
		BuildID string `json:"build_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return uuid.Nil, err
	}

	return uuid.Parse(result.BuildID)
}

func (c *HTTPBuilderClient) CancelBuild(ctx context.Context, buildID uuid.UUID) error {
	httpCtx, cancel := c.timeoutContext(ctx)
	defer cancel()

	req, err := c.newRequest(httpCtx, http.MethodPost, "/api/v1/builds/"+buildID.String()+"/cancel", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return builderHTTPError(resp)
}

func (c *HTTPBuilderClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	internalauth.SetScopeHeadersFromContext(req.Header, ctx)
	req.Header.Set(internalauth.HeaderName, c.internalToken)
	if requestID := logging.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set(logging.RequestIDHeader, requestID)
	}
	return req, nil
}

func (c *HTTPBuilderClient) timeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(c.cfg.Get().ComposeBuilderHTTPTimeoutSeconds)*time.Second)
}

func builderHTTPError(resp *http.Response) error {
	message := fmt.Sprintf("builder returned status %d", resp.StatusCode)

	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		message = body.Error
	}

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return apperrors.New(apperrors.ErrBadRequest, message)
	case http.StatusUnauthorized:
		return apperrors.New(apperrors.ErrUnauthorized, message)
	case http.StatusForbidden:
		return apperrors.New(apperrors.ErrForbidden, message)
	case http.StatusNotFound:
		return apperrors.New(apperrors.ErrNotFound, message)
	case http.StatusConflict:
		return apperrors.New(apperrors.ErrConflict, message)
	default:
		return apperrors.New(apperrors.ErrInternal, apperrors.ErrInternal.Error())
	}
}
