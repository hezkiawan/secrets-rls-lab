# Presentation script, part 1: secrets management (slides 1–22)

Spoken script, slide by slide, up to where RLS starts (slide 23).
- *(…)* = what to do or show.
- 🖥 = live demo.
- About 30 minutes including demos.

---

## Slide 1 · Cover

"Hi everyone. Over the last few days I researched two questions you gave me.

One: which secrets manager should we run, HashiCorp Vault or OpenBao?

Two: how can the database itself make sure each customer only sees their own data? That's Row Level Security, in Postgres and in Firebase.

I didn't only read about it. I built a lab with our own stack (Go with Fiber, Postgres, PgBouncer) and tested everything I'll show you today."

---

## Slide 2 · Three risks today

"First, why should we care?

Today our secrets live in **.env files** on every server. They get copied, they end up in backups, maybe in git history. We can't see who read them, and changing one means editing every server.

Second, we use **one shared database login** for everything. Nobody rotates it, because that would break everything. If it leaks, it opens all our data. And it probably owns the tables, which will matter later for RLS.

Third, the **tenant filter only exists in our Go code**. Every query has to remember `WHERE company_id = …`. If one query forgets, one customer sees another customer's data.

Three problems, three answers: a secrets manager for the .env files, database logins handed out by that secrets manager, and Row Level Security. Let's start with the secrets manager."

---

## Slide 3 · Same family: OpenBao is the open-source fork

"A secrets manager does four things:
- keeps every secret in **one encrypted place**
- makes apps **log in** and only gives them what their policy allows
- can create **temporary credentials**, like a database user that expires
- **logs every access**

Vault and OpenBao both do this, because they're the same product family.

Vault was open source until version 1.14. In 2023 HashiCorp changed the licence to the Business Source License. The community took the last open-source code and continued it as **OpenBao**. In 2025 IBM bought HashiCorp, and OpenBao moved to the Linux Foundation.

So it's the same design and the same API, with different owners and licences."

---

## Slide 4 · Licence, ownership and footprint

"What's actually different?

**Licence.** Both are free for us to use internally. Vault's licence explicitly allows internal use. But only OpenBao is truly open source, and that was our priority.

**Owner.** OpenBao is a community project; Vault belongs to IBM.

The most important row is the **API**: it's the same. I ran the exact same scripts and Go code against both, and only the command name changed. So whatever we choose, we're not locked in.

Resources are about the same. OpenBao's image is smaller, but that's a detail."

---

## Slide 5 · Features

"The shaded rows are features that are **free in OpenBao but paid in Vault**:
- **Namespaces**: a separate area per product, with its own admins. I tested it, and Vault's free version answered 'enterprise-only feature'.
- **Standby reads**: standby servers can answer reads too.
- **Control groups**: a second person has to approve a sensitive request.

Everything we actually need is in all three: storing secrets, database credentials, app login, clustering, auto-unseal.

What only Vault Enterprise has (replication between regions, Sentinel policies, automated backups) we don't need at our size."

---

## Slide 6 · Verdict: use OpenBao

"So my recommendation is **OpenBao**:
1. It's open source.
2. Nothing we need is behind a paywall.
3. There's no lock-in, because the API is the same as Vault's.

The honest trade-off: support comes from the community and it moves fast, so we read release notes and test upgrades in staging.

Before I show how we deployed it, let me explain how OpenBao works inside, because the rest only makes sense with these basics."

---

## Slide 7 · Everything is encrypted, and it starts sealed

"OpenBao never stores anything in plain text. Think of a safe in an office.

Your **data** (secrets, policies, everything) is the documents in the safe. The **encryption key** is the key to the safe. That key is kept in a small lockbox, locked by the **root key**. And the **seal** is the rule for how you get the root key back.

**Init** happens once, like formatting a new disk: it creates these keys.

After that, every time the server starts, it's **sealed**: running, but it doesn't have the root key in memory, so it can't read anything. **Unsealing** means giving it back the root key. Then it can work."

*(If asked: "The root key isn't the root token. The root key unlocks data; the root token is an admin login.")*

---

## Slide 8 · Three ways to unseal

"There are three ways to give the server its root key back.

**Shamir** is the default. At init the unseal key is split into five pieces, and any three can rebuild it. You give them to different people, so nobody can unseal alone. But after every restart, three people have to type their piece.

**Static key**: the key sits in a config file or environment variable. Automatic, but anyone who can read that file can unseal. Lab only.

**Transit**: another OpenBao server keeps a key and decrypts our root key when asked. Automatic, and the key never leaves that other server.

Cloud key services work the same way, but Tencent's has no plugin. So on Tencent we use transit."

---

## Slide 9 · Unsealing by hand, with Shamir

"This is the manual way.
- Once: `bao operator init` prints five unseal keys and the first admin token.
- After a restart, `bao status` says Sealed: true.
- Three people each run `bao operator unseal` and paste their key.
- After the third, Sealed becomes false.

With auto-unseal there's no unseal command at all; the server does it itself at start.

And the 'dev mode' we started with skips all of this: it starts unsealed and keeps everything in memory. That's for learning only."

---

## Slide 10 · Secret engines

"Inside OpenBao, the actual work is done by **secret engines**: plugins mounted at a path, a bit like folders with a different machine behind each.
- `secret/` **stores** values; our app secrets live there.
- `database/` doesn't store passwords at all. It **creates a temporary Postgres user** when asked, and deletes it later.
- `transit/` **encrypts and decrypts** for others without giving out the key. That's what our unsealer uses.
- `pki/` issues certificates; we might use it for TLS later.

A path is: the mount you chose, then a route the engine defines, then your own name. For example `secret/data/kouventa/app`."

---

## Slide 11 · Access: prove who you are, get a token

"Nobody uses OpenBao anonymously.

First you **log in** through an auth method. Apps like our API use **AppRole**: a role ID, like a username, and a secret ID, like a password. People use the company login or a username and password.

The login gives you a **token**. It's like a session: it says which **policies** you have and how long it's valid. Every request after that carries the token.

There's one special token: the **root token** from init. It has no limits, so you use it once for setup and then revoke it."

---

## Slide 12 · Policies

"A **policy** says what a token may do, per path. Anything not listed is **forbidden**.

Our API's policy says:
- read everything under `kouventa`
- **deny** the admin folder inside it
- read database credentials for its own role

When several rules match a path, the most specific one wins, and deny always wins on its own path.

I tested it: the API can read its own secrets, but gets 403 for the admin folder and for another product's secrets. This is what you called 'filtering'."

---

## Slide 13 · Putting it together

"So every request passes the same gates:
0. The server must be **unsealed**.
1. The app **logs in**.
2. It gets a **token** with its policy.
3. It asks for a **path**.
4. OpenBao **checks the policy**; if it's not allowed, 403.
5. The **engine** does the work, and it all goes into the audit log.

Seal, access and policy, engine. Now let's see how we deployed it."

---

## Slide 14 · Each box is one VM

"This is the production-like setup I built. In the lab every box is a container, but the config files are the ones you'd put on VMs.

The **Go API** only knows one address: **HAProxy**, the load balancer. HAProxy sends traffic only to the **active** OpenBao server.

There are **three OpenBao servers** in a cluster. Each has a full copy of the data, one is the leader, and the cluster survives losing one. For production, five is recommended.

The **unsealer** on the right is a small separate OpenBao that unlocks the three servers when they start.

OpenBao also talks to **Postgres** directly, to create temporary database users. The API reaches Postgres through **PgBouncer**. And backups are snapshots copied to object storage."

---

## Slide 15 · The unsealer setup · 🖥

"The unsealer is prepared once, by a script called `setup.sh`:
1. It starts and unlocks itself. In the lab with a static key; in production people would do this.
2. It's initialized.
3. It turns on the **transit** engine and creates a key called **autounseal**. That key can never be exported.
4. It creates a **policy** that only allows encrypt and decrypt with that key.
5. It creates a **token** with that policy, for our three servers."

🖥 *`docker compose -f reference/docker-compose.yml logs unsealer-setup`* → "…and it ends with 'unsealer ready'."

---

## Slide 16 · The key never leaves the unsealer

"So how does a server actually unlock?

Each server needs **two things**:
- Its config file, `bao-1.hcl`, says **where and which key**: the unsealer's address, and the key autounseal.
- Its environment variable `BAO_TOKEN` gives it **permission**: the token the setup script created.

Unlike AppRole, there's no login step here. The token itself is the credential, created in advance.

At startup the server sends its **encrypted** root key to the unsealer, with the token. The unsealer decrypts it with autounseal and sends back the root key. Now the server can open its data.

The important part: **the autounseal key never leaves the unsealer**, and the token can do nothing except encrypt and decrypt with it.

In the lab the token has a fixed name written in docker-compose. In production it would be random and delivered securely, never in git."

---

## Slide 17 · Init once, then every node unseals itself · 🖥

"With the unsealer ready, we start the main cluster:
1. We run **init** once, on bao-1. It creates bao-1's keys and asks the unsealer to encrypt its root key. With auto-unseal we get **recovery keys** instead of unseal keys, plus the first root token.
2. bao-1 unseals and becomes the **leader**.
3. bao-2 and bao-3 **join automatically**, get the data, ask the unsealer, and become standbys.

HAProxy checks each server: **200** means active and gets the traffic, 429 means standby, 503 means sealed.

After a full restart this all happens by itself; a new leader was elected in about 15 seconds."

🖥 *HAProxy page `localhost:8404`* → "One green: the active one. The red ones aren't broken; they're standbys."

---

## Slide 18 · What configure.sh sets up · 🖥

"A new cluster is empty. One script, `configure.sh`, sets up everything our API needs, and it's safe to run again:
- `secret/` with the app's secrets, including a **.env file we imported** automatically
- the **policy** we saw
- the **AppRole** login for the API
- the **database engine**: it connects to Postgres as a special admin user, and immediately **changes that admin's password**, so only OpenBao knows it. Then a role that creates temporary users for the API.

The root token is used here once. In production we'd revoke it afterwards."

🖥 *OpenBao UI `localhost:8300`* → show `secret/kouventa/app`, `secret/kouventa/env`, Policies, `database/`.

---

## Slide 19 · Only one package talks to OpenBao

"How does our Go API use all this?

Only **one package**, `openbao/`, talks to OpenBao. It hands values to the others:
- the temporary database user to `database/`
- the key that signs user logins to `auth/`

The important point: **a user's request never calls OpenBao**. OpenBao is used at startup and for renewals in the background. So in our real services, only one package would change."

---

## Slide 20 · Startup: six steps · 🖥

"At startup:
1. Read settings: addresses only, no secrets.
2. Log in with AppRole and get a token.
3. Read the secrets.
4. Ask for a temporary database user. OpenBao creates it in Postgres on the spot.
5. Connect to Postgres through PgBouncer with that user.
6. Start the background renewals and serve requests.

Each step is one HTTP call. If any step fails, the API refuses to start, which is better than running without secrets.

I also wrote a version of this in one single file, top to bottom, so it's easy to read."

🖥 *`go run ./cmd/simple`* → "Each step gets a check mark: login, secrets, the admin folder refused, a temporary DB user, and RLS filtering." *(Ctrl+C after step 5.)*

---

## Slide 21 · Background loops

"Tokens and database users **expire on purpose**, so the API keeps them alive.

The **token** is renewed every 30 seconds, forever, while the API runs (`background.go`).

The **database user** has a maximum lifetime. Before it ends, the API asks for a **new** user, opens a new connection pool, and swaps it in. Requests never notice.

One thing we only found by testing: OpenBao deletes a database user when the **token** that created it expires. With a normal token, every re-login killed our fresh database user. The fix is a **periodic token**, one that can be renewed forever."

🖥 *API log* → point at `[db] user … renewed` and `[db] now using new user …`.

---

## Slide 22 · What we tested · 🖥 HA demo

"Does it hold up when things break?
- **40 out of 40** requests succeeded while the database user rotated, the token renewed, and I **killed the active server**.
- After restarting **everything**, it unlocked itself and elected a leader in about **15 seconds**.
- Backups restored, a killed server rejoined by itself, and the same code ran on OpenBao and on Vault.

Let me show you the failover live."

🖥 HA demo:
1. *`docker compose -f reference/docker-compose.yml stop <active node>`*
2. *Refresh `:8404`* → "Another server is green now."
3. *Refresh `/status`* → "`openbao_active_node` changed." Click around the demo page → "Still works."
4. *`docker compose -f reference/docker-compose.yml start <node>`* → "It comes back as a standby and unseals itself."

"That's the secrets side. Now the second topic: Row Level Security."

➜ *Slide 23*
