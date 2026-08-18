package httpapi

import "github.com/gin-gonic/gin"

type successEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

func writeSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, successEnvelope{Data: data, RequestID: requestIDFrom(c)})
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorEnvelope{Error: errorDetail{Code: code, Message: message}, RequestID: requestIDFrom(c)})
}
