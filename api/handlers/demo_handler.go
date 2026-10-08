package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/middleware"
	"secrets-rls-lab/api/openbao"
	"secrets-rls-lab/api/services"
)

// GET /demo/no-context: same query, but the code "forgot" to say who is asking.
func NoContextDemo(c fiber.Ctx) error {
	conversations, err := services.ListConversationsWithoutContext()
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(fiber.Map{
		"count":         len(conversations),
		"conversations": conversations,
		"explanation":   "No request context was set, so the policies match nothing: fail-safe, zero rows.",
	})
}

// GET /demo/as-owner: same query, but connected as the table OWNER.
func AsOwnerDemo(c fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	conversations, err := services.ListConversationsAsOwner(user)
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(fiber.Map{
		"count":         len(conversations),
		"conversations": conversations,
		"explanation": "Connected as the table owner. Without FORCE ROW LEVEL SECURITY the owner " +
			"bypasses every policy and sees all companies. Run reference/postgres/demo/force-rls.sql, then try again.",
	})
}

// GET /status: which temporary DB user is in use, and which OpenBao node is active.
func Status(c fiber.Ctx) error {
	dbUser, since := database.CurrentUser()

	return c.JSON(fiber.Map{
		"database_user":        dbUser,
		"database_user_in_use": time.Since(since).Round(time.Second).String(),
		"openbao_active_node":  openbao.ActiveNode(),
	})
}
