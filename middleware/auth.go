package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// Pre-allocated error response bytes to avoid per-request JSON marshalling.
var unauthorizedBody = []byte(`{"error":"unauthorized"}`)

// Auth returns a Fiber middleware that validates requests against a static bearer token.
func Auth(token string) fiber.Handler {
	bearerToken := "Bearer " + token
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" || auth != bearerToken {
			return c.Status(fiber.StatusUnauthorized).Send(unauthorizedBody)
		}
		return c.Next()
	}
}
