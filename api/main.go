// Command api is the lab's Go + Fiber v3 service.
//
// M1: at startup it logs into OpenBao with AppRole and loads its secrets.
// If it can't, it refuses to start ("fail fast"): an API without its secrets
// is broken, and it's better to know immediately than at the first request.
package main

import (
	"context"
	"log"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/logger"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"

	"secrets-rls-lab/api/internal/config"
	"secrets-rls-lab/api/internal/openbao"
)

func main() {
	cfg := config.Load()
	health := openbao.NewHealthChecker(cfg.OpenBaoAddr, 2*time.Second)

	// ---- Startup: log in and load secrets (before accepting any request) ----
	startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bao, err := openbao.NewClient(cfg.OpenBaoAddr, cfg.KVMount)
	if err != nil {
		log.Fatal(err)
	}
	if err := bao.LoginAppRole(startupCtx, cfg.RoleIDFile, cfg.SecretIDFile); err != nil {
		log.Fatalf("%v\n\nHint: is OpenBao running and bootstrapped? Run:  docker compose run --rm bootstrap", err)
	}
	secrets, err := bao.LoadAppSecrets(startupCtx, cfg.Product)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Logged into OpenBao with AppRole and loaded %d secrets for %q", len(secrets.Fingerprints()), cfg.Product)
	// Note: we never log secret VALUES. Fingerprints only.

	// ---- HTTP server ----
	app := fiber.New(fiber.Config{AppName: "secrets-rls-lab API (M1)"})
	app.Use(recoverer.New())
	app.Use(logger.New())

	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())                   // GET /livez
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(healthcheck.Config{ // GET /readyz
		Probe: func(c fiber.Ctx) bool {
			_, err := health.Check(c.Context())
			return err == nil
		},
	}))

	// GET /status — what the API knows about itself (no secret values!)
	app.Get("/status", func(c fiber.Ctx) error {
		h, err := health.Check(c.Context())
		openbaoInfo := fiber.Map{"address": cfg.OpenBaoAddr, "ready": err == nil}
		if h != nil {
			openbaoInfo["health"] = h
		}
		if err != nil {
			openbaoInfo["error"] = err.Error()
		}

		token, err := bao.LookupSelf(c.Context())
		if err != nil {
			openbaoInfo["token_error"] = err.Error()
		} else {
			openbaoInfo["token"] = token
		}

		return c.JSON(fiber.Map{
			"api":         "ok",
			"environment": cfg.AppEnv,
			"product":     cfg.Product,
			"openbao":     openbaoInfo,
			"secrets":     secrets.Fingerprints(),
		})
	})

	// GET /demo/read?path=otherproduct/app — try to read ANY path with our token.
	// Shows the policy in action: some paths allowed, others denied.
	app.Get("/demo/read", func(c fiber.Ctx) error {
		path := c.Query("path")
		if path == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "add ?path=..., e.g. /demo/read?path=otherproduct/app",
			})
		}
		return c.JSON(bao.TryRead(c.Context(), path))
	})

	app.Get("/demo/list", func(c fiber.Ctx) error {
		names, err := bao.ListSecrets(c.Context(), cfg.Product)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"product": cfg.Product, "secrets": names})
	})

	log.Printf("API listening on http://localhost:%s  (env: %s, OpenBao: %s)", cfg.Port, cfg.AppEnv, cfg.OpenBaoAddr)
	log.Fatal(app.Listen(":" + cfg.Port))
}
