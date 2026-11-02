package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/auth"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
)

func Auth(routeRoles ...auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(authorizationHeader)
		if token == "" || !strings.HasPrefix(token, bearerPrefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "error": "Unauthorized"})
			return
		}

		token = strings.TrimPrefix(token, bearerPrefix)

		userClaims, err := auth.GetClaims(token)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "error": "Token expired"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "error": "Unauthorized"})
			return
		}

		if !auth.HasRequiredRoles(userClaims.Roles, routeRoles) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "error": "Forbidden"})
			return
		}
	}
}
