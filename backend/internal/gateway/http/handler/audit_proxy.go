package handler

import (
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

type auditResponseWriter struct {
	gin.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func AuditedProxy(core *CoreHandler, proxy gin.HandlerFunc, action, resourceType, sourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		writer := &auditResponseWriter{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = writer
		proxy(c)

		outcome := auditlog.OutcomeSuccess
		errorCode := ""
		if writer.status >= http.StatusBadRequest {
			outcome = auditlog.OutcomeFailure
			errorCode = http.StatusText(writer.status)
		}
		core.recordAudit(c, auditInput{
			Action:       action,
			ResourceType: resourceType,
			Outcome:      outcome,
			ErrorCode:    errorCode,
			DetailsJSON:  auditlog.SafeDetailsJSON(map[string]string{auditlog.DetailSourceType: sourceType}),
		})
	}
}
