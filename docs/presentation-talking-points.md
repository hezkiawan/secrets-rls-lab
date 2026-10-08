# Presentation talking points: Secrets Management & RLS

**How to use:**
- **Bold** = say this sentence (almost) word for word.
- Bullets = points to cover in your own words.
- 🖥 = switch to the live demo.
- ➜ = transition to the next slide.

**Timing:** about 25 minutes of slides plus about 10 minutes of demo. If you're short on time, skip the slides marked *(optional)*.

---

## Section 1: Why

### 1. Cover (30s)
- **"Today I'll answer two questions the team gave me: which secrets manager should we run, OpenBao or HashiCorp Vault, and how can the database itself make sure each customer only sees their own data."**
- Everything I show was built and tested in a lab repo, `secrets-rls-lab`, using our own stack: Go with Fiber, PostgreSQL, PgBouncer and Firebase.
- ➜ "First, why we need this at all."

### 2. Three risks today (1 min)
- **".env files on every server"**
  - Secrets are copied around, sit in backups and maybe in git history.
  - Nobody knows who read what.
  - Changing one means editing every server.
- **"One shared database login"**
  - Nobody rotates it, because that would break everything.
  - If it leaks, it reaches all data.
  - It probably created, so owns, the tables. That matters later for RLS.
- **"The tenant filter lives only in Go."**
  - Every query has to remember `WHERE company_id = …`.
  - One forgotten clause shows one customer another customer's data.
- **"Three problems, three answers: a secrets manager, database logins from that secrets manager, and Row Level Security."**
- ➜ "Let's start with the secrets manager."

---

## Section 2: OpenBao vs Vault

### 3. Same family: OpenBao is the open-source fork (1.5 min)
- **What a secrets manager does:**
  - One encrypted place for every secret.
  - Apps log in and get only what their policy allows.
  - It can create temporary credentials, like database users that expire.
  - It logs every access.
- **The history:**
  - Vault was open source until version 1.14.
  - In August 2023 HashiCorp moved Vault 1.15 and later to the Business Source License.
  - The community took the last open-source code, between 1.14.8 and 1.14.9, and continued it as **OpenBao**.
  - IBM completed its acquisition of HashiCorp in February 2025.
  - OpenBao moved to the OpenSSF, part of the Linux Foundation, in May 2025.
- **"So they're the same design and the same API, two projects with different owners and licences."**
- The version numbers (Vault 2.1.1, OpenBao 2.7.1) are both 2.x by coincidence; they don't match feature-wise.
- ➜ "So what actually differs?"

### 4. Licence, ownership and footprint (1.5 min)
- **"Both are free for us to use internally. Only OpenBao is open source, and that was the team's priority."**
- Vault's BSL allows production use. It forbids offering Vault to others in competition with IBM's paid products, and the licence explicitly says internal use isn't that.
- Paid options:
  - Vault has Enterprise and a managed cloud version.
  - OpenBao has no paid edition, but vendors sell support.
- **"The most important row: same API. We ran the exact same scripts and Go code against both. Only the command name and environment variable names changed."**
- Footprint, measured in dev mode (relative only):
  - Image size: 80 MB vs 188 MB.
  - Idle memory: about the same.
- Production sizing is the same for both.
- ➜ "Now features."

### 5. Features (2 min)
- **"The shaded rows are free in OpenBao but paid in Vault."**
  - **Namespaces:** a separate mini-OpenBao per product, with its own admins. We tested it; Vault Community answered "enterprise-only feature".
  - **Standby reads:** standby nodes answer read requests themselves, so there's more capacity.
  - **Control groups:** a request on a sensitive path waits for a second person's approval. New in OpenBao 2.7.
- **The core is the same in all three:** KV secrets, database credentials, encryption, AppRole login, the Raft cluster, and transit auto-unseal (what we use).
- **Only Vault Enterprise has:**
  - replication between whole clusters in different regions
  - Sentinel policy-as-code
  - automated snapshots
  - data masking
  - syncing secrets into other clouds
- **"None of those is needed at our scale."**
- Small note: in OpenBao 2.7, cloud-KMS and HSM unseal are separate plugins. Transit is built in.
- ➜ "So my recommendation…"

### 6. Verdict: Use OpenBao (45s)
- **"Use OpenBao."** Three reasons:
  1. It's truly open source.
  2. Nothing we need is behind a paywall, and namespaces per product come free.
  3. No lock-in: same API, so moving to Vault later is a configuration change.
- Be honest about the trade-off: community support, and it moves fast (2.7 changed several things). So we read release notes and test upgrades in staging.
- ➜ "Here's what running it looks like."

---

## Section 3: Architecture (M5)

### 7. Each box is one VM (1.5 min)
- **"In the lab every box is a container, but the config files are the same ones you'd put on VMs."**
- Left to right:
  - The **Go API** only knows one address: **HAProxy**.
  - HAProxy checks each node and sends traffic only to the **active** one. Standbys get none.
- **Three OpenBao nodes** in a Raft cluster:
  - Each holds a full copy of the data and one is the leader.
  - Three nodes survive losing one. Five survive two, which OpenBao recommends for production.
- The **unsealer** is a small separate OpenBao that holds the unlock key. Next slide.
- OpenBao talks to **Postgres** directly, to create and delete temporary database users.
- The API reaches Postgres through **PgBouncer**.
- **Backups** are Raft snapshots copied to object storage, e.g. Tencent COS.
- ➜ "First: how does a cluster unlock itself?"

### 8. The unsealer (1.5 min) · 🖥 demo cue
- **"OpenBao encrypts everything on disk. After every restart it's 'sealed' and needs its key."**
- Options:
  - People type in key shares.
  - A cloud key service unlocks it.
  - Another OpenBao unlocks it.
- **"Tencent sells a KMS, but neither OpenBao nor Vault has a plugin for it. So we use transit: a small OpenBao holds the key."**
- Walk the 5 steps:
  1. Start.
  2. Initialize.
  3. Create the key `autounseal`, which can never be exported.
  4. A policy that allows only encrypt and decrypt with it.
  5. A token the nodes use.
- Chicken and egg: something must unlock the unsealer.
  - Lab: a static key.
  - Production: people with key shares, or an HSM.
  - It restarts rarely, so humans unlock **one** small server, not every node.
- 🖥 *Optional:* `docker compose -f reference/docker-compose.yml logs unsealer-setup` → ends with "unsealer ready".
- ➜ "With the unsealer running, the main cluster."

### 9. Init once, then every node unseals itself (1.5 min) · 🖥 demo cue
1. **"Init happens once ever, like formatting a disk."**
   - It runs through bao-1 directly, because the load balancer has no active node yet.
   - It returns recovery keys and the first root token.
2. bao-1 asks the unsealer to decrypt its key → unsealed → **leader**.
3. bao-2 and bao-3 keep trying to join by themselves (`retry_join`) → they get the data, ask the unsealer → **standbys**.
- HAProxy health codes:
  - **200** = active, gets traffic
  - **429** = healthy standby
  - **503** = sealed
  - **501** = not initialized
- **"After a full restart this all happens automatically. Electing a leader took about 15 seconds."**
- 🖥 HAProxy page `localhost:8404`: one green node, two "down". Down here means standby (429), not broken.
- ➜ "An empty cluster does nothing yet. We configure it as code."

### 10. What configure.sh sets up (1.5 min) · 🖥 demo cue
- **"Everything the API needs is created by one script, and it's safe to run again."**
- Walk the tree:
  - `secret/`: the app's secrets, including the `.env` file we imported.
  - A **policy**.
  - An **AppRole** login for the API.
  - The **database engine** with its connection and the role for temporary users.
- **How to read a path:** mount / route / name, e.g. `secret` / `data` / `kouventa/app`. Every path is also an HTTP address, `/v1/secret/data/…`.
- **"The policy is what 'filtering' means here":**
  - It reads `kouventa/*` and is denied `kouventa/admin/*`.
  - It may get database users and renew its leases.
  - Anything else → 403.
- The root token is used once for setup. In production you revoke it, and admins log in with their own accounts.
- 🖥 OpenBao UI `localhost:8300`: show `secret/kouventa/app`, `env`, the policy, `database/`.
- ➜ "Now, how does our Go code use all this?"

---

## Section 4: Go client

### 11. Only one package talks to OpenBao (1 min)
- **"In the API, exactly one package, `openbao/`, talks to OpenBao. It hands values to the others."**
  - The temporary database user goes to `database/`, which opens the pool to PgBouncer.
  - The JWT signing key goes to `auth/`, which signs and verifies user logins.
- **"A user request never calls OpenBao."** OpenBao is used at startup and by background renewals only.
- In our real services, only one package would need to change.
- ➜ "What happens at startup?"

### 12. Startup: six steps (1.5 min) · 🖥 demo cue
1. Read settings: addresses only, **no secrets**.
2. Log in with AppRole → token.
3. Read secrets → JWT key, Meta token.
4. Ask for a temporary DB user → OpenBao creates it in Postgres on the spot.
5. Open the pool through PgBouncer as that user. It's a member of `app_runtime`, so RLS applies.
6. Start the background loops, then serve.
- **"Each step is one HTTP call. The CLI we used in the lab sends exactly the same requests."**
- **"If any step fails, the API refuses to start. Better than running without secrets."**
- 🖥 `go run ./cmd/simple`: the whole integration in one file, one ✔ per step. Ctrl+C after step 5.
- ➜ "Credentials expire on purpose. So who renews them?"

### 13. Background loops (1.5 min)
- **Token:** periodic, renewed every 30 s, forever, while the API runs (`background.go`).
- **Database user:**
  1. Renew its lease.
  2. When it reaches its max lifetime, get a **new** user.
  3. Open a new pool and **swap it in** (`database.SetPool`).
  4. The old pool closes 5 s later, after its queries finish.
- **"Requests never notice: they always take the current pool."**
- **"One thing we only found by testing":**
  - A database user is revoked when the **token that created it** expires.
  - With a normal token, every re-login cut off the fresh DB user about 30 seconds later.
  - The fix is a periodic token.
- 🖥 Point at the API log: `[db] user … renewed`, and `[db] now using new user …` if one appeared.
- ➜ "So does it hold up when things break?"

### 14. What we tested (1 min) · 🖥 HA demo cue
- **40 of 40 requests** succeeded while the DB user rotated, the token renewed, and we **killed the active node**.
- **About 15 seconds** to elect a leader after restarting everything. Data was intact and nodes unsealed automatically.
- **0 rows** if the code forgets to say who is asking: it fails safe.
- **Same code** on OpenBao and Vault.
- 🖥 HA demo:
  1. `docker compose -f reference/docker-compose.yml stop <active node>`.
  2. Refresh `:8404` → another node turns green.
  3. Refresh `/status` → `openbao_active_node` changed. The demo page still works.
  4. `start` the node again → it rejoins and unseals itself.
- ➜ "Second topic: Row Level Security."

---

## Section 5: RLS

### 15. The database filters rows for every query (1 min)
- **"Today every query must remember the tenant filter. With RLS, rules on the table add it automatically, even to the query someone forgot."**
- Rules are written once, in SQL.
- It's a safety net: Go still checks permissions, and RLS catches the mistakes.
- RLS exists since PostgreSQL 9.5. We used 18.
- ➜ "How do you turn it on?"

### 16. Turning it on, and who it applies to (1.5 min) · 🖥 demo cue
- `ENABLE`: policies apply. A table with no policies then returns **no rows**.
- **"But some roles skip RLS":**
  - superusers and `BYPASSRLS` roles, always
  - the **table owner**, unless you add `FORCE`
- **"The trap for us: our shared login probably owns the tables. RLS would look on and filter nothing."**
- Fix:
  - a separate **owner** role for migrations
  - a separate **runtime** role for the app
  - plus `FORCE` as a safety net
- OpenBao's temporary users belong to the runtime role.
- 🖥 Demo page **"As owner"** → all companies leak. Run `force-rls.sql` → click again → filtered.
- On TencentDB the admin role also has `BYPASSRLS`, so the app needs its own role there too.
- ➜ "What does a policy look like?"

### 17. Policy syntax (1.5 min)
- Template, top to bottom:
  - **name** and **table**
  - **AS**: permissive or restrictive (next slide)
  - **FOR**: which commands
  - **TO**: which roles
  - **USING**: condition on existing rows
  - **WITH CHECK**: condition on rows being written
- **Our real policy:** `tenant_isolation`, restrictive, all commands: company must equal **my** company.
- **Where "my company" comes from:** `current_company_id()` reads a setting the API puts on each transaction.
  - **"If it's not set, it's NULL, which matches nothing: zero rows, never all rows."**
- ➜ "Why restrictive?"

### 18. Walls and doors (1.5 min)
- **"Every restrictive policy must pass, AND at least one permissive one."**
- **Restrictive = walls.** Our only wall is tenant isolation: nothing crosses companies, whatever the doors allow.
- **Permissive = doors**, different ways in:
  - agents: their own conversations plus the unassigned queue
  - supervisors: everything in their company
  - updates and inserts
- Result:
  - Ana (agent) sees **3** rows; Sari (supervisor) sees all **5** Acme rows.
  - Neither sees Bumi Coffee's.
- **"With only walls and no door, nobody sees anything."**
- ➜ "Two clauses that are easy to mix up."

### 19. USING vs WITH CHECK (1.5 min) · 🖥 demo cue
- **USING = rows that already exist.** If a row fails, it's silently hidden.
  - Budi tries to update Ana's conversation → 0 rows changed → our API says **404**.
- **WITH CHECK = what you write.** If it fails, Postgres raises an error.
  - Inserting into another company → error 42501 → our API says **403**.
- **UPDATE uses both:**
  - USING decides which rows you can touch.
  - WITH CHECK decides what they may become, so you can't move a row to another company.
- 🖥 Demo page: **spoof** button → 403; Budi edits Ana's → not found; **no context** → 0 rows.
- ➜ "Let's put it together for one request."

### 20. One request, HTTP to rows (1.5 min) · 🖥 demo cue
- Walk the 7 boxes:
  1. Request with JWT.
  2. Fiber handler.
  3. Get the user from the verified JWT.
  4. **Tell Postgres** company, user and role.
  5. Plain `SELECT`.
  6. Postgres applies the policies.
  7. Only allowed rows come back.
- **"The `true` in `set_config` means 'this transaction only'. At COMMIT it's gone. That's what makes it safe behind PgBouncer, where the same connection serves another user right after. A plain `SET` would leak."**
- Values come from the verified JWT, **never** from the request body.
- OpenBao isn't involved per request.
- 🖥 Demo page: switch Ana → Sari → Dewi. Same SQL, different rows.
- ➜ "And Firebase?"

---

## Section 6: Firebase

### 21. Security Rules are RLS for the frontend only (1.5 min)
- **"Firebase has no RLS, but Security Rules do the same job for the frontend: every read and write from the web or Flutter app is checked against a rules file."**
- **"Our Go backend uses the Admin SDK, which skips all rules, like a database superuser. So backend endpoints must check permissions in Go."**
- Three things to remember:
  1. **Who is asking:** custom claims on the user's login token (company, role), set by our backend.
  2. **Rules are not filters:**
     - A query that *could* return a forbidden document fails completely.
     - The app must ask only for what it's allowed, e.g. "assigned to me".
     - Postgres would just drop the rows instead.
  3. **Admin SDK = superuser.**
- ➜ "Here's what the rules look like."

### 22. Our rules in Firestore (1.5 min)
- Same cases as the Postgres demo:
  - **read:** own company only; supervisors all; agents their own plus unassigned
  - **update:** agents change only `status` on their own conversations
  - **delete:** never from the app
- The table maps the concepts:
  - `resource.data` = USING
  - `request.resource.data` = WITH CHECK
  - the claim = our `set_config`
  - the Admin SDK = superuser
- The repo has the full rules, the Realtime Database version, and emulator tests. *(If you ran `npm test`: "the tests pass". If not: "running the tests is the next step".)*
- ➜ "How do we roll this out?"

---

## Section 7: Wrap-up

### 23. Rollout in small, reversible steps (1.5 min)
- **"Each step is useful on its own and can be undone."**
- **OpenBao track:**
  1. Staging cluster from the reference files, with TLS. Practise failover and restore.
  2. One service's `.env` into OpenBao.
  3. Production, then service by service.
  4. OpenBao rotates the **existing** shared DB password. No app redesign.
- **Database and Firebase track:**
  1. Split the database roles. **Can start now**, and RLS needs it anyway.
  2. RLS table by table, staging first.
  3. Temporary DB users per service.
  4. Firebase rule tests and a review of Admin SDK endpoints. **Can start now**.
- ➜ "To sum up…"

### 24. In short + Questions (30s + Q&A)
- **"OpenBao on our VMs, with transit auto-unseal and one AppRole per service. Split the database roles, then RLS with a tenant wall. Firebase rules for the frontend, permission checks in Go for the backend."**
- **"Everything is in the repo: the report with sources, a deployment guide, the RLS and Firebase guides, and the reference setup that runs all of this locally."**

---

## Likely questions

| Question | Answer |
|---|---|
| What if OpenBao goes down? | Running apps keep working until their token or DB user expires (hours in production). New starts fail. That's why it's a 3–5 node cluster |
| Does every request call OpenBao? | No. Only startup, renewals every 30 s, and DB user rotation |
| Can the API read another product's secrets? | No, its policy only allows `kouventa/*`. We showed the 403 |
| Why not just put the DB password in OpenBao? | That's step 1 (static role, rotated by OpenBao). Temporary users go further: one per service, short-lived, auditable |
| Can we switch to Vault later? | Yes. Same API and Go client; we tested it |
| Do we need Vault Enterprise features? | Not at our scale. The main one would be replication across regions |
| Why not Tencent KMS for unsealing? | Tencent sells a KMS, but neither OpenBao nor Vault has a plugin for it. Transit works anywhere |
| Is RLS slow? | It adds the policy condition to queries. Index the columns it uses, e.g. `company_id`, and check with `EXPLAIN` |
| What about the `secret_id`, isn't that a secret too? | Yes, "secret zero". In production the deploy pipeline delivers it once, single-use, wrapped and bound to the app servers' IPs. The periodic token means it's only needed at startup |
| Managed Postgres (TencentDB)? | RLS works. No real superuser; its admin role bypasses RLS, so the app needs its own role. Dynamic users must be tested there |
