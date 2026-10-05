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

	// The database pool. Its credentials come from OpenBao, never from a config file:
	//   DB_CREDS=static  (M3) a fixed app_runtime login stored in KV
	//   DB_CREDS=dynamic (M5) a temporary Postgres user per app instance, rotated automatically
	appCtx, stop := context.WithCancel(context.Background())
	defer stop()
	pools := &db.Pools{}
	if cfg.DBCreds == "dynamic" {
		if err := startDynamicDB(startupCtx, appCtx, bao, pools, cfg); err != nil {
			log.Fatalf("%v\n\nHint: is the reference cluster configured?  (reference/README.md)", err)
		}
	} else {
		login, err := bao.LoadDBLogin(startupCtx, cfg.Product+"/db")
		if err != nil {
			log.Fatalf("%v\n\nHint: run  docker compose run --rm bootstrap /bootstrap/m3-setup.sh", err)
		}
		pool, err := db.Open(startupCtx, cfg.DBAddr, cfg.DBName, login.Username, login.Password)
		if err != nil {
			log.Fatalf("%v\n\nHint: is the reference stack running?  docker compose -f reference/docker-compose.yml up -d", err)
		}
		pools.Swap(pool, login.Username)
	}
	defer pools.CloseAll()
	user, _ := pools.Info()
	log.Printf("Connected to Postgres at %s as %q (via PgBouncer, %s credentials)", cfg.DBAddr, user, cfg.DBCreds)

	// Keep the OpenBao login alive: renew the token, log in again when it reaches max TTL.
	go bao.KeepLoggedIn(appCtx)

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
			return pools.Current().Ping(c.Context()) == nil
		},
	}))

	registerM1Routes(app, cfg, health, bao, secrets, pools)
	registerConversationRoutes(app, pools, ownerPool, tokens)

	// The tiny demo page (api/web/index.html) at http://localhost:3000/
	app.Get("/*", static.New("./web"))

	log.Printf("API listening on http://localhost:%s  (env: %s, OpenBao: %s)", cfg.Port, cfg.AppEnv, cfg.OpenBaoAddr)
	log.Fatal(app.Listen(":" + cfg.Port))
}

// registerM1Routes: what the API knows about itself, and the OpenBao policy demo.
func registerM1Routes(app *fiber.App, cfg config.Config, health *openbao.HealthChecker,
	bao *openbao.Client, secrets *openbao.AppSecrets, pools *db.Pools) {

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
		if leader, err := bao.Leader(c.Context()); err == nil && leader != "" {
			openbaoInfo["active_node"] = leader // which cluster node is serving (M5)
		}
		dbUser, since := pools.Info()
		return c.JSON(fiber.Map{
			"api":         "ok",
			"environment": cfg.AppEnv,
			"product":     cfg.Product,
			"openbao":     openbaoInfo,
			"secrets":     secrets.Fingerprints(),
			"database": fiber.Map{
				"credentials": cfg.DBCreds,
				"user":        dbUser,
				"in_use_for":  time.Since(since).Round(time.Second).String(),
			},
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

// startDynamicDB gets a temporary Postgres user from OpenBao, opens a pool with it, and
// starts a background loop that keeps it alive and replaces it before it expires:
//
//	get creds → open pool → renew lease … renew … (max TTL reached) → get NEW creds →
//	open new pool → swap (old pool closes gracefully) → repeat
func startDynamicDB(startupCtx, appCtx context.Context, bao *openbao.Client, pools *db.Pools, cfg config.Config) error {
	connect := func(ctx context.Context) (*openbao.Lease, error) {
		lease, login, err := bao.DynamicDBCreds(ctx, cfg.DBRole)
		if err != nil {
			return nil, err
		}
		pool, err := db.Open(ctx, cfg.DBAddr, cfg.DBName, login.Username, login.Password)
		if err != nil {
			return nil, err
		}
		pools.Swap(pool, login.Username)
		log.Printf("[db] now using temporary user %q (lease %ds)", login.Username, lease.TTL)
		return lease, nil
	}

	lease, err := connect(startupCtx)
	if err != nil {
		return err
	}
	go func() {
		for {
			_ = bao.WatchLease(appCtx, lease, "database")
			if appCtx.Err() != nil {
				return
			}
			log.Printf("[db] credentials reached their max TTL → rotating to a new user")
			for attempt := 1; ; attempt++ {
				ctx, cancel := context.WithTimeout(appCtx, 15*time.Second)
				next, err := connect(ctx)
				cancel()
				if err == nil {
					lease = next
					break
				}
				log.Printf("[db] rotation attempt %d failed: %v", attempt, err)
				select {
				case <-appCtx.Done():
					return
				case <-time.After(time.Duration(min(attempt, 10)) * time.Second):
				}
			}
		}
	}()
	return nil
}
