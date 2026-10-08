// The secrets-rls-lab API: a small support desk that gets ALL its secrets from OpenBao
// and lets PostgreSQL Row Level Security decide which rows each user may see.
//
// Startup, top to bottom:
//
//	STEP 1  read settings (addresses only, no secrets)
//	STEP 2  log in to OpenBao with AppRole
//	STEP 3  read the app's secrets
//	STEP 4  get a temporary database user from OpenBao
//	STEP 5  connect to Postgres (through PgBouncer) with that user
//	STEP 6  start the background jobs that keep everything alive
//	STEP 7  register the web routes and start the server
//
// Run it (from the api folder, with the reference stack running):   go run .
package main

import (
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"

	"secrets-rls-lab/api/auth"
	"secrets-rls-lab/api/config"
	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/handlers"
	"secrets-rls-lab/api/middleware"
	"secrets-rls-lab/api/openbao"
)

func main() {
	// STEP 1 — Settings ---------------------------------------------------------------
	cfg := config.Load()

	// STEP 2 — Log in to OpenBao ------------------------------------------------------
	err := openbao.Connect(cfg.OpenBaoAddr)
	if err != nil {
		log.Fatal("could not create OpenBao client: ", err)
	}
	err = openbao.Login(cfg.RoleIDFile, cfg.SecretIDFile)
	if err != nil {
		log.Fatal("could not log in to OpenBao (is the reference stack running and configured?): ", err)
	}
	log.Println("STEP 2: logged in to OpenBao at", cfg.OpenBaoAddr)

	// STEP 3 — Read the app's secrets -------------------------------------------------
	secrets, err := openbao.ReadAppSecrets(cfg.Product)
	if err != nil {
		log.Fatal("could not read app secrets: ", err)
	}
	auth.SetSigningKey(secrets.JWTSigningKey) // used to sign user logins
	log.Println("STEP 3: read secrets from secret/" + cfg.Product + "/app")

	// STEP 4 — Get a temporary database user ------------------------------------------
	creds, err := openbao.GetDBCredentials(cfg.DBRole)
	if err != nil {
		log.Fatal("could not get database credentials: ", err)
	}
	log.Println("STEP 4: got temporary database user", creds.Username)

	// STEP 5 — Connect to Postgres with that user ------------------------------------
	pool, err := database.Connect(cfg.DBAddr, cfg.DBName, creds.Username, creds.Password)
	if err != nil {
		log.Fatal("could not connect to Postgres: ", err)
	}
	database.SetPool(pool, creds.Username)
	log.Println("STEP 5: connected to Postgres at", cfg.DBAddr, "as", creds.Username)

	// LAB ONLY: a second connection as the table OWNER, for the "as owner" RLS demo.
	ownerUser, ownerPassword, err := openbao.ReadDBOwnerLogin(cfg.Product)
	if err != nil {
		log.Fatal("could not read the owner login: ", err)
	}
	database.OwnerPool, err = database.Connect(cfg.DBAddr, cfg.DBName, ownerUser, ownerPassword)
	if err != nil {
		log.Fatal("could not connect as owner: ", err)
	}

	// STEP 6 — Background jobs (see background.go) ------------------------------------
	go keepTokenAlive(cfg)
	go keepDatabaseUserAlive(cfg, creds)
	log.Println("STEP 6: background jobs started")

	// STEP 7 — Web routes -------------------------------------------------------------
	app := fiber.New()
	app.Use(logger.New()) // print every request

	// No login needed:
	app.Get("/demo/users", handlers.ListDemoUsers)
	app.Post("/login", handlers.Login)
	app.Get("/status", handlers.Status)

	// Login needed: middleware.RequireLogin runs first.
	app.Get("/me", middleware.RequireLogin, handlers.Me)
	app.Get("/conversations", middleware.RequireLogin, handlers.ListConversations)
	app.Patch("/conversations/:id", middleware.RequireLogin, handlers.UpdateStatus)
	app.Post("/conversations", middleware.RequireLogin, handlers.CreateConversation)
	app.Get("/demo/no-context", middleware.RequireLogin, handlers.NoContextDemo)
	app.Get("/demo/as-owner", middleware.RequireLogin, handlers.AsOwnerDemo)

	// The demo page (web/index.html).
	app.Get("/*", static.New("./web"))

	log.Println("STEP 7: listening on http://localhost:" + cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
