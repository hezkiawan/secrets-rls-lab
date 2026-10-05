package main

// M3: the conversations API. Read the handlers and notice what's MISSING:
// no handler filters by company or user. Postgres Row Level Security does it,
// using the "who" that db.WithTenant passes to it inside each transaction.

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"secrets-rls-lab/api/internal/auth"
	"secrets-rls-lab/api/internal/db"
)

func registerConversationRoutes(app *fiber.App, pool, ownerPool *pgxpool.Pool, tokens *auth.Issuer) {
	// ---- Demo login (no password — DEMO ONLY) ---------------------------------------

	// GET /demo/users — the seeded users, for the login dropdown.
	app.Get("/demo/users", func(c fiber.Ctx) error {
		users, err := db.ListUsers(c.Context(), pool)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(users)
	})

	// POST /login {"email": "ana@acme.test"} → {"token": "..."}
	app.Post("/login", func(c fiber.Ctx) error {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.Bind().Body(&req); err != nil || req.Email == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": `send {"email": "..."}`})
		}
		user, err := db.FindUserByEmail(c.Context(), pool, req.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unknown user"})
		}
		if err != nil {
			return serverError(c, err)
		}
		token, err := tokens.Issue(user.Who)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"token": token, "user": user})
	})

	// ---- Everything below requires a valid token ------------------------------------
	// `requireLogin` runs before each handler that lists it: no valid JWT → 401.
	requireLogin := tokens.Require()

	// GET /me — who the token says you are.
	app.Get("/me", requireLogin, func(c fiber.Ctx) error {
		return c.JSON(auth.WhoFrom(c))
	})

	// GET /conversations — the query has NO "WHERE company_id = ...". RLS filters it.
	app.Get("/conversations", requireLogin, func(c fiber.Ctx) error {
		var list []db.Conversation
		err := db.WithTenant(c.Context(), pool, auth.WhoFrom(c), func(tx pgx.Tx) error {
			var err error
			list, err = db.ListConversations(c.Context(), tx)
			return err
		})
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"count": len(list), "conversations": list})
	})

	// PATCH /conversations/:id {"status": "closed"}
	app.Patch("/conversations/:id", requireLogin, func(c fiber.Ctx) error {
		var req struct {
			Status string `json:"status"`
		}
		if err := c.Bind().Body(&req); err != nil || req.Status == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": `send {"status": "open|pending|closed"}`})
		}
		var changed int64
		err := db.WithTenant(c.Context(), pool, auth.WhoFrom(c), func(tx pgx.Tx) error {
			var err error
			changed, err = db.UpdateStatus(c.Context(), tx, c.Params("id"), req.Status)
			return err
		})
		if err != nil {
			return serverError(c, err)
		}
		if changed == 0 {
			// RLS hides rows you may not touch, so "not allowed" and "doesn't exist" look the same.
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "not found or not allowed (RLS: agents may only update their own conversations)",
			})
		}
		return c.JSON(fiber.Map{"updated": changed})
	})

	// POST /conversations {"customer_name","channel","subject"[, "spoof_company_id"]}
	// spoof_company_id exists ONLY for the demo: try to write into another company.
	app.Post("/conversations", requireLogin, func(c fiber.Ctx) error {
		var req struct {
			CustomerName   string `json:"customer_name"`
			Channel        string `json:"channel"`
			Subject        string `json:"subject"`
			SpoofCompanyID string `json:"spoof_company_id"`
		}
		if err := c.Bind().Body(&req); err != nil || req.CustomerName == "" || req.Subject == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "customer_name and subject are required"})
		}
		if req.Channel == "" {
			req.Channel = "webchat"
		}
		who := auth.WhoFrom(c)
		companyID := who.CompanyID
		if req.SpoofCompanyID != "" {
			companyID = req.SpoofCompanyID
		}
		err := db.WithTenant(c.Context(), pool, who, func(tx pgx.Tx) error {
			return db.CreateConversation(c.Context(), tx, companyID, req.CustomerName, req.Channel, req.Subject)
		})
		if db.IsRLSViolation(err) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "blocked by Row Level Security",
				"postgres": err.Error(),
			})
		}
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"created": true, "company_id": companyID})
	})

	// ---- Safety-net demos ------------------------------------------------------------

	// GET /demo/no-context — the same query, but the code "forgot" to say who is asking.
	app.Get("/demo/no-context", requireLogin, func(c fiber.Ctx) error {
		var list []db.Conversation
		err := pgx.BeginTxFunc(c.Context(), pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
			var err error
			list, err = db.ListConversations(c.Context(), tx) // no set_config → no context
			return err
		})
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{
			"count":         len(list),
			"conversations": list,
			"explanation":   "No request context was set, so the policies match nothing: fail-safe, zero rows.",
		})
	})

	// GET /demo/as-owner — the same request, but connected as the table OWNER (app_owner).
	app.Get("/demo/as-owner", requireLogin, func(c fiber.Ctx) error {
		var list []db.Conversation
		err := db.WithTenant(c.Context(), ownerPool, auth.WhoFrom(c), func(tx pgx.Tx) error {
			var err error
			list, err = db.ListConversations(c.Context(), tx)
			return err
		})
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{
			"count":         len(list),
			"conversations": list,
			"explanation": "Connected as the table owner. Without FORCE ROW LEVEL SECURITY the owner " +
				"bypasses every policy and sees all companies. Run reference/postgres/demo/force-rls.sql, then try again.",
		})
	})
}

func serverError(c fiber.Ctx, err error) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
}
