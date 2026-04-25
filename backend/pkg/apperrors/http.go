package apperrors

import "github.com/gin-gonic/gin"

type ErrorResponse struct {
	Error string `json:"error"`
}

func Respond(c *gin.Context, statusCode int, err error) {
	c.AbortWithStatusJSON(statusCode, ErrorResponse{Error: SafeMessage(err)})
}
