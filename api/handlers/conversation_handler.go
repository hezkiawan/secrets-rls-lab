package handlers

import (
	"github.com/gofiber/fiber/v3"

	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/middleware"
	"secrets-rls-lab/api/services"
)

// GET /conversations: the rows this user may see (RLS decides).
func ListConversations(c fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	conversations, err := services.ListConversations(user)
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(fiber.Map{"count": len(conversations), "conversations": conversations})
}

// PATCH /conversations/:id  {"status": "closed"}
func UpdateStatus(c fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var body struct {
		Status string `json:"status"`
	}
	err := c.Bind().Body(&body)
	if err != nil || body.Status == "" {
		return c.Status(400).JSON(fiber.Map{"error": `send {"status": "open|pending|closed"}`})
	}

	changed, err := services.ChangeStatus(user, c.Params("id"), body.Status)
	if err != nil {
		return serverError(c, err)
	}
	if !changed {
		return c.Status(404).JSON(fiber.Map{
			"error": "not found or not allowed (RLS: agents may only update their own conversations)",
		})
	}
	return c.JSON(fiber.Map{"updated": 1})
}

// POST /conversations  {"customer_name", "channel", "subject"}
func CreateConversation(c fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var input services.NewConversation
	err := c.Bind().Body(&input)
	if err != nil || input.CustomerName == "" || input.Subject == "" {
		return c.Status(400).JSON(fiber.Map{"error": "customer_name and subject are required"})
	}

	companyID, err := services.CreateConversation(user, input)
	if database.IsRLSViolation(err) {
		return c.Status(403).JSON(fiber.Map{
			"error":    "blocked by Row Level Security",
			"postgres": err.Error(),
		})
	}
	if err != nil {
		return serverError(c, err)
	}
	return c.Status(201).JSON(fiber.Map{"created": true, "company_id": companyID})
}
