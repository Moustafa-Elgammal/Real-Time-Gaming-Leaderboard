package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const internalTokenHeader = "X-Internal-Token"

// InternalAuth returns a Gin middleware that enforces a shared secret between
// this service and the upstream game service. The game service must include
// the header:
//
//	X-Internal-Token: <INTERNAL_API_KEY>
//
// When key is empty the middleware is a no-op, which keeps local development
// and docker-compose setups working without configuration.
func InternalAuth(key string) gin.HandlerFunc {
	if key == "" {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.GetHeader(internalTokenHeader) != key {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
