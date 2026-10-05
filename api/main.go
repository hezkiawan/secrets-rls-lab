// Command api is the lab's Go + Fiber v3 service.
//
// Startup (fail fast — if anything is missing, refuse to start):
//  1. log into OpenBao with AppRole                         (M1)
//  2. load the app's secrets: JWT key, API tokens           (M1)
//  3. load the database login from OpenBao, open the pool   (M3)
//
// Then it serves: health/status (M0–M1), the policy demo (M1),
// and the conversations API protected by Row Level Security (M3), plus a tiny web page.
package main

import (
	"context"
	"log"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/logger"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	"secrets-rls-lab/api/internal/auth"
	"secrets-rls-lab/api/internal/config"
	"secrets-rls-lab/api/internal/db"
	"secrets-rls-lab/api/internal/openbao"
)

func main() {
	cfg := config.Load()
	health := openbao.NewHealthChecker(cfg.OpenBaoAddr, 2*time.Second)

	// ---- Startup ---------------------------------------------------------------------
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	// M3: the runtime DB login lives in OpenBao too — never in a config file.
	runtimeLogin, err := bao.LoadDBLogin(startupCtx, cfg.Product+"/db")
	if err != nil {
		log.Fatalf("%v\n\nHint: run  docker compose run --rm bootstrap /bootstrap/m3-setup.sh", err)
	}
	pool, err := db.Open(startupCtx, cfg.DBAddr, cfg.DBName, runtimeLogin.Username, runtimeLogin.Password)
	if err != nil {
		log.Fatalf("%v\n\nHint: is the reference stack running?  docker compose -f reference/docker-compose.yml up -d", err)
	}
	defer pool.Close()
	log.Printf("Connected to Postgres at %s as %q (via PgBouncer)", cfg.DBAddr, runtimeLogin.Username)

	// LAB ONLY: a second pool as the table OWNER, to demonstrate the "owner bypasses RLS"
	// trap. A real app must never hold the owner's credentials.
	ownerLogin, err := bao.LoadDBLogin(startupCtx, cfg.Product+"/db-owner")
	if err != nil {
		log.Fatal(err)
	}
	ownerPool, err := db.Open(startupCtx, cfg.DBAddr, cfg.DBName, ownerLogin.Username, ownerLogin.Password)
	if err != nil {
		log.Fatal(err)
	}
	defer ownerPool.Close()

	tokens := auth.NewIssuer(secrets.JWTSigningKey())

	// ---- HTTP server -------------------------------------------------------------------
	app := fiber.New(fiber.Config{AppName: "secrets-rls-lab API (M3)"})
	app.Use(recoverer.New())
	app.Use(logger.New())

	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(healthcheck.Config{
		Probe: func(c fiber.Ctx) bool {
			if _, err := health.Check(c.Context()); err != nil {
				return false
			}
			return pool.Ping(c.Context()) == nil
		},
	}))

	registerM1Routes(app, cfg, health, bao, secrets)
	registerConversationRoutes(app, pool, ownerPool, tokens)

	// The tiny demo page (api/web/index.html) at http://localhost:3000/
	app.Get("/*", static.New("./web"))

	log.Printf("API listening on http://localhost:%s  (env: %s, OpenBao: %s)", cfg.Port, cfg.AppEnv, cfg.OpenBaoAddr)
	log.Fatal(app.Listen(":" + cfg.Port))
}

// registerM1Routes: what the API knows about itself, and the OpenBao policy demo.
func registerM1Routes(app *fiber.App, cfg config.Config, health *openbao.HealthChecker,
	bao *openbao.Client, secrets *openbao.AppSecrets) {

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

	app.Get("/demo/read", func(c fiber.Ctx) error {
		path := c.Query("path")
		if path == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "add ?path=..., e.g. /demo/read?path=otherproduct/app",
			})
		}
		return c.JSON(bao.TryRead(c.Context(), path))
	})
}
