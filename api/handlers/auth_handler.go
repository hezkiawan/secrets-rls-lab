// Package handlers turns HTTP requests into service calls, and results into JSON.
// No SQL and no business rules here.
package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"secrets-rls-lab/api/middleware"
	"secrets-rls-lab/api/services"
)

// GET /demo/users: everyone you can log in as (demo page buttons).
func ListDemoUsers(c fiber.Ctx) error {
	users, err := services.ListDemoUsers()
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(users)
}

// POST /login  {"email": "ana@acme.test"}  →  {"token": "...", "user": {...}}
func Login(c fiber.Ctx) error {
	var body struct {
		Email string `json:"email"`
	}
	err := c.Bind().Body(&body)
	if err != nil || body.Email == "" {
		return c.Status(400).JSON(fiber.Map{"error": `send {"email": "..."}`})
	}

	token, user, err := services.Login(body.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.Status(401).JSON(fiber.Map{"error": "unknown user"})
	}
	if err != nil {
		return serverError(c, err)
	}

	return c.JSON(fiber.Map{"token": token, "user": user})
}

// GET /me: who the token says you are.
func Me(c fiber.Ctx) error {
	return c.JSON(middleware.CurrentUser(c))
}

// serverError sends a 500 with the error message (fine for a lab).
func serverError(c fiber.Ctx, err error) error {
	return c.Status(500).JSON(fiber.Map{"error": err.Error()})
}
