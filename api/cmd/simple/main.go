// cmd/simple — the WHOLE OpenBao + RLS integration in one file, top to bottom.
//
// This is a READING version of api/. It does the same important things, in order,
// with no web server, no helper packages and no clever tricks:
//
//	STEP 1  Log in to OpenBao with AppRole
//	STEP 2  Read the app's secrets from KV
//	STEP 3  Ask OpenBao for a temporary Postgres user
//	STEP 4  Connect to Postgres (through PgBouncer) with that user
//	STEP 5  Run queries with Row Level Security: as Ana, then with no context
//	STEP 6  Keep the token and the DB user alive (renew every minute)
//
// Run it against the reference cluster (PowerShell, from the api folder):
//
//	$env:OPENBAO_ADDR="http://127.0.0.1:8300"
//	go run ./cmd/simple
//
// What this version LEAVES OUT on purpose (the full api/ has it):
//   - An HTTP server and user logins (JWT)           → api/conversations.go, internal/auth/
//   - Replacing the DB user before it expires        → main.go startDynamicDB, internal/db/pools.go
//   - Logging in again if the token is ever lost     → internal/openbao/lifecycle.go KeepLoggedIn
//   - A connection POOL (it uses one connection)     → internal/db/db.go Open
//
// Everything here only uses the official libraries:
//
//	github.com/openbao/openbao/api/v2           (talk to OpenBao)
//	github.com/openbao/openbao/api/auth/approle/v2
//	github.com/jackc/pgx/v5                      (talk to Postgres)
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openbao/openbao/api/auth/approle/v2"
	bao "github.com/openbao/openbao/api/v2"
)

func main() {
	ctx := context.Background()

	// Settings. Only addresses and file names: NO secrets here.
	openbaoAddr := envOr("OPENBAO_ADDR", "http://127.0.0.1:8300")     // HAProxy in front of the cluster
	roleIDFile := envOr("OPENBAO_ROLE_ID_FILE", ".reference/role_id") // written by configure.sh
	secretIDFile := envOr("OPENBAO_SECRET_ID_FILE", ".reference/secret_id")
	dbAddr := envOr("DB_ADDR", "127.0.0.1:6432") // PgBouncer

	// =====================================================================================
	// STEP 1 — Log in to OpenBao with AppRole
	//   HTTP:  PUT /v1/auth/approle/login   {role_id, secret_id}   →  a token
	//   CLI:   bao write auth/approle/login role_id=… secret_id=…
	// =====================================================================================
	config := bao.DefaultConfig()
	config.Address = openbaoAddr
	client, err := bao.NewClient(config)
	if err != nil {
		log.Fatalf("creating OpenBao client: %v", err)
	}

	roleIDBytes, err := os.ReadFile(roleIDFile)
	if err != nil {
		log.Fatalf("reading role ID: %v", err)
	}
	roleID := strings.TrimSpace(string(roleIDBytes)) // remove a trailing newline

	appRoleLogin, err := approle.NewAppRoleAuth(roleID, &approle.SecretID{FromFile: secretIDFile})
	if err != nil {
		log.Fatalf("preparing AppRole login: %v", err)
	}
	loginResult, err := client.Auth().Login(ctx, appRoleLogin)
	if err != nil {
		log.Fatalf("STEP 1 failed — AppRole login: %v", err)
	}
	// From here on, the client sends this token with EVERY request (header X-Vault-Token).
	fmt.Printf("STEP 1 ✔ logged in. Token policies: %v, valid for %ds (periodic: renewable forever)\n",
		loginResult.Auth.Policies, loginResult.Auth.LeaseDuration)

	// =====================================================================================
	// STEP 2 — Read the app's secrets from KV
	//   HTTP:  GET /v1/secret/data/kouventa/app
	//   CLI:   bao kv get secret/kouventa/app
	// =====================================================================================
	appSecrets, err := client.KVv2("secret").Get(ctx, "kouventa/app")
	if err != nil {
		log.Fatalf("STEP 2 failed — reading secrets: %v", err)
	}
	jwtSigningKey := appSecrets.Data["jwt_signing_key"].(string)
	metaAPIToken := appSecrets.Data["meta_api_token"].(string)
	// Never print secret VALUES. Lengths are enough to prove we got them.
	fmt.Printf("STEP 2 ✔ read secret/kouventa/app: jwt_signing_key (%d chars), meta_api_token (%d chars)\n",
		len(jwtSigningKey), len(metaAPIToken))

	// The policy is enforced: this read must FAIL with 403 (path not allowed).
	_, err = client.KVv2("secret").Get(ctx, "kouventa/admin/break-glass")
	fmt.Printf("         reading kouventa/admin/break-glass is refused, as the policy says: %v\n", err != nil)

	// =====================================================================================
	// STEP 3 — Ask OpenBao for a temporary Postgres user
	//   HTTP:  GET /v1/database/creds/kouventa-app
	//   CLI:   bao read database/creds/kouventa-app
	//   OpenBao runs CREATE ROLE "v-approle-…" … IN ROLE app_runtime in Postgres right now.
	// =====================================================================================
	dbCreds, err := client.Logical().ReadWithContext(ctx, "database/creds/kouventa-app")
	if err != nil {
		log.Fatalf("STEP 3 failed — getting DB credentials: %v", err)
	}
	dbUser := dbCreds.Data["username"].(string)
	dbPassword := dbCreds.Data["password"].(string)
	dbLeaseID := dbCreds.LeaseID // we need this to renew the user later (STEP 6)
	fmt.Printf("STEP 3 ✔ got temporary DB user %s (lease %ds)\n", dbUser, dbCreds.LeaseDuration)

	// =====================================================================================
	// STEP 4 — Connect to Postgres (through PgBouncer) with that user
	// =====================================================================================
	dbURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(dbUser, dbPassword), // escapes special characters safely
		Host:   dbAddr,
		Path:   "supportdesk",
	}
	dbConfig, err := pgx.ParseConfig(dbURL.String() + "?sslmode=disable") // LAB ONLY: no TLS
	if err != nil {
		log.Fatalf("parsing DB address: %v", err)
	}
	// PgBouncer (transaction pooling) and pgx's cached prepared statements don't mix.
	// "Exec" mode sends each query on its own. (Same setting as internal/db/db.go.)
	dbConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	conn, err := pgx.ConnectConfig(ctx, dbConfig)
	if err != nil {
		log.Fatalf("STEP 4 failed — connecting to Postgres: %v", err)
	}
	defer conn.Close(ctx)
	fmt.Printf("STEP 4 ✔ connected to Postgres at %s as %s\n", dbAddr, dbUser)

	// =====================================================================================
	// STEP 5 — Queries with Row Level Security
	//   The SQL has NO "WHERE company_id = …". Postgres filters the rows using the policies
	//   in reference/postgres/initdb/03-rls.sql and the 3 settings we give it.
	//   In the full API these 3 values come from the user's verified JWT.
	// =====================================================================================

	// 5a. As Ana (agent at Acme Retail): expect her 2 conversations + the unassigned one.
	tx, err := conn.Begin(ctx) // BEGIN
	if err != nil {
		log.Fatal(err)
	}
	_, err = tx.Exec(ctx, `SELECT set_config('app.company_id', $1, true),
	                              set_config('app.user_id',    $2, true),
	                              set_config('app.role',       $3, true)`,
		"11111111-1111-1111-1111-111111111111", // Acme Retail
		"a0000000-0000-0000-0000-00000000000a", // Ana
		"agent")
	// The final `true` = "only for THIS transaction" (same as SET LOCAL).
	// At COMMIT the settings disappear, so PgBouncer can't leak them to another request.
	if err != nil {
		log.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT customer_name FROM support.conversations ORDER BY customer_name`)
	if err != nil {
		log.Fatal(err)
	}
	anaSees, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		log.Fatal(err)
	}
	err = tx.Commit(ctx) // COMMIT — the 3 settings are gone now
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("STEP 5 ✔ as Ana, the same SELECT returns %d rows: %v\n", len(anaSees), anaSees)

	// 5b. Same query, but nobody told Postgres who is asking → the policies match nothing.
	var countWithoutContext int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM support.conversations`).Scan(&countWithoutContext)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("         with NO context it returns %d rows (fail-safe: not \"all rows\")\n", countWithoutContext)

	// =====================================================================================
	// STEP 6 — Keep the token and the DB user alive
	//   Token: HTTP PUT /v1/auth/token/renew-self      CLI: bao token renew
	//   DB:    HTTP PUT /v1/sys/leases/renew {lease_id} CLI: bao lease renew <lease_id>
	//   If we stopped renewing, OpenBao would expire them and DROP the Postgres user.
	//   (The full API does this with the library's LifetimeWatcher instead of a plain loop.)
	// =====================================================================================
	fmt.Println("STEP 6   renewing every 60s. Press Ctrl+C to stop.")
	for {
		time.Sleep(60 * time.Second)

		tokenRenewal, err := client.Auth().Token().RenewSelfWithContext(ctx, 0) // 0 = "the normal period"
		if err != nil {
			log.Fatalf("token renewal failed: %v", err)
		}

		leaseRenewal, err := client.Sys().RenewWithContext(ctx, dbLeaseID, 0)
		if err != nil {
			log.Fatalf("DB lease renewal failed: %v", err)
		}

		var stillWorks int
		err = conn.QueryRow(ctx, `SELECT 1`).Scan(&stillWorks)
		if err != nil {
			log.Fatalf("DB connection failed: %v", err)
		}

		fmt.Printf("%s  token renewed (%ds left) · DB user renewed (%ds left) · DB query OK\n",
			time.Now().Format("15:04:05"), tokenRenewal.Auth.LeaseDuration, leaseRenewal.LeaseDuration)

		// The DB user has a MAXIMUM lifetime (6 minutes in the lab). Renewals give less and
		// less time as it gets close. The full API now fetches a NEW user and swaps it in.
		// This simple version just stops and explains.
		if leaseRenewal.LeaseDuration < 70 {
			fmt.Println("         The DB user reaches its maximum lifetime within a minute, so it can't be renewed further.")
			fmt.Println("         The full API (api/main.go startDynamicDB) would now get a new user (STEP 3)")
			fmt.Println("         and switch to it. This simple version stops here.")
			return
		}
	}
}

// envOr returns the environment variable, or a default if it is not set.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
