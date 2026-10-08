// Package middleware has code that runs BEFORE a handler.
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"secrets-rls-lab/api/auth"
	"secrets-rls-lab/api/models"
)

// RequireLogin checks the "Authorization: Bearer <token>" header.
// No valid token → 401 and the handler never runs.
// Valid token    → the user is stored in the request, then the handler runs.
func RequireLogin(c fiber.Ctx) error {
	header := c.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return c.Status(401).JSON(fiber.Map{"error": "missing Bearer token, log in first"})
	}
	tokenString := strings.TrimPrefix(header, "Bearer ")

	user, err := auth.ParseToken(tokenString)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid token: " + err.Error()})
	}

	c.Locals("user", user) // store it for the handler
	return c.Next()        // go on to the handler
}

// CurrentUser returns the user stored by RequireLogin.
func CurrentUser(c fiber.Ctx) models.User {
	user, _ := c.Locals("user").(models.User)
	return user
}
