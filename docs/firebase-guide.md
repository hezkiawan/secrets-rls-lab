# Can we do "RLS" in Firebase? — guide

**Short answer:** yes for **client** access. **Firebase Security Rules** do the same job as Postgres RLS for reads and writes from the web and Flutter apps. But:
1. They do **not** apply to the **Admin SDK** (our Go backend).
2. They are **not filters**: a query that *could* return a forbidden document fails completely.

**Files:** `reference/firebase/firestore.rules`, `reference/firebase/database.rules.json`, and tests in `reference/firebase/test/rules.test.mjs`. They encode the same support-desk cases as M3, so the two can be compared directly. One difference: in Firebase, agents may change **only** `status`. The Postgres version also lets an agent un-assign.
**Sources (checked 2026-10-06):** [Firestore rules & queries](https://firebase.google.com/docs/firestore/security/rules-query), [Firestore conditions](https://firebase.google.com/docs/firestore/security/rules-conditions), [RTDB core syntax](https://firebase.google.com/docs/database/security/core-syntax), [RTDB conditions](https://firebase.google.com/docs/database/security/rules-conditions), [Admin SDK (RTDB)](https://firebase.google.com/docs/database/admin/start), [Firestore server libraries](https://firebase.google.com/docs/firestore/security/get-started), [Custom claims](https://firebase.google.com/docs/auth/admin/custom-claims), [Rules unit tests](https://firebase.google.com/docs/rules/unit-tests), [SQL Connect](https://firebase.google.com/docs/data-connect).

---

## 1. Who is checked where

```
Flutter / Next.js (client SDK) ──► Firestore / Realtime DB      ← Security Rules CHECK every request
Go backend (Admin SDK)         ──► Firestore / Realtime DB      ← Rules are BYPASSED (full access)
```

- Firestore docs: *"The server client libraries bypass all Cloud Firestore Security Rules"*. Use IAM to limit them.
- RTDB docs: the Admin SDK can *"read and write all data, regardless of Security Rules"*. Exception: it can be started with `databaseAuthVariableOverride` to act as a limited user.
- **So, for the team:** reads from the frontend → protect with Rules. Reads from the Go backend → the Go code must check permissions itself (like any API).

---

## 2. Postgres RLS ↔ Firebase Rules

| Concept | PostgreSQL RLS | Firestore Rules | Realtime DB Rules |
|---|---|---|---|
| Rules live in | The database (`CREATE POLICY`) | `firestore.rules` file, deployed with Firebase CLI | `database.rules.json` |
| "Who is asking" | `set_config('app.company_id', …)` per transaction | `request.auth.uid`, `request.auth.token.<claim>` | `auth.uid`, `auth.token.<claim>` |
| Default | Deny (once enabled) | Deny | Deny |
| Existing row (≈ `USING`) | `USING (…)` | `resource.data` | `data` |
| New row (≈ `WITH CHECK`) | `WITH CHECK (…)` | `request.resource.data` | `newData` |
| Combine rules | Permissive OR, restrictive AND | Several `allow` → OR | **Cascade:** access granted at a parent can't be revoked deeper |
| Unfiltered query | **Silently filtered** to allowed rows | **Whole query fails** | **Whole read fails** |
| Validation | `CHECK` constraints | Conditions in `allow create/update` | `.validate` rules |
| Field-level | Column privileges / views | `diff().affectedKeys().hasOnly([...])` | Rules per child key |
| Bypass | Superuser, owner, `BYPASSRLS` | **Admin SDK** | **Admin SDK** |
| Testing | SQL as the runtime role | Emulator + `@firebase/rules-unit-testing` | Same |

---

## 3. Tenant / role info: custom claims

Firebase Auth tokens can carry **custom claims**, e.g. `{ company_id: "acme", role: "agent" }`:
- Set them **only from the backend** with the Admin SDK (`SetCustomUserClaims` in Go).
- Size limit: **1000 bytes**. Keep them to IDs and roles, not data.
- Claims reach the client in the next ID token: after sign-in, a forced refresh, or the hourly token refresh (ID tokens last 1 hour). After changing a user's role, force a token refresh.

This is the Firebase equivalent of our JWT → `set_config` step.

---

## 4. The rules we wrote (same cases as M3)

| Case | Firestore | Realtime DB |
|---|---|---|
| Tenant isolation | Path `companies/{companyId}/…` + `request.auth.token.company_id == companyId` | `$companyId` + `auth.token.company_id === $companyId` |
| Agent reads own + unassigned | `resource.data.assigned_to == request.auth.uid \|\| resource.data.assigned_to == null` | Same with `data.child('assigned_to')` |
| Supervisor reads all (own company) | `request.auth.token.role == 'supervisor'` | Read at the list level (cascades down) |
| Agent **lists** own | Query **must** have `where('assigned_to','==',uid)` | Query **must** be `orderByChild('assigned_to').equalTo(uid)`, checked with `query.orderByChild` / `query.equalTo` |
| Can't create in another company | `companyId` is the path; must match the claim | Same |
| Agent changes only `status` | `diff(resource.data).affectedKeys().hasOnly(['status'])` | `.write` requires other fields unchanged (`newData.child(x).val() === data.child(x).val()`) |
| Valid values only | `status in ['open','pending','closed']` | `.validate` with regex |
| No client deletes | `allow delete: if false` | `.write` requires `newData.exists()` |

**Run the tests** (Node 22+ and **Java 21+** needed, because current firebase-tools requires 21; the emulators download on first run):
```powershell
cd reference/firebase
npm install
npm test
```
Each test is a case from the M3 demo. The `demo-supportdesk` project ID means it never touches a real Firebase project.

> ⚠️ I couldn't run the emulators in my build environment (download blocked). The rules and tests are written against the current docs and library API, and a separate review traced every assertion by hand, but they have **not been executed yet**. Run `npm test` once before relying on them.

**Realtime DB limitation:** agents can list their own conversations with a query, but not the **unassigned queue**. A rule allowing `equalTo(null)` would also allow a query with no `equalTo`, which would expose everything. Use a sentinel value (`assigned_to: "unassigned"`) or a separate `queue/` node.

---

## 5. Gotchas

- **Rules are not filters.** The client must query with the same condition the rules check. Typical error: an agent opens the inbox without `where(assigned_to == me)` → *permission denied*.
- **RTDB cascade:** a `.read: true` high in the tree opens everything below it. Grant at the **lowest** level possible.
- **Missing fields** in Firestore rules cause an error, and an error in the deciding part of a condition → denied. Store explicit `null` (we do for `assigned_to`).
- **`get()` / `exists()` limits:** a rule may look up other documents (e.g. a membership doc), up to **10** per single-document read or query, **20** for batches and transactions. Over that → denied. Prefer claims for hot paths.
- **Admin SDK = superuser.** Any backend endpoint that reads Firebase for a user must check that user's company and role in Go.
- **Service-account key** for the Admin SDK is a secret → store it in OpenBao (done in M1: `secret/kouventa/firebase`).
- **App Check** complements Rules: it blocks requests that don't come from your real app (abuse protection). It is not an authorization tool.

---

## 6. Related: Firebase SQL Connect

*Firebase Data Connect* is now called **Firebase SQL Connect**. It puts a managed **Cloud SQL for PostgreSQL** behind a Firebase-style API.
- Authorization is defined per query or mutation with an `@auth` directive (levels like `USER`, `USER_EMAIL_VERIFIED`) plus server-side filters and CEL expressions.
- Its docs do **not** describe it as Postgres RLS.
- Only relevant if the team wants Postgres data accessed directly from clients. Our setup uses a Go API instead.

---

## 7. Recommendation

1. Keep **Security Rules** for every collection or path the frontend reads. Write rules like ours: tenant claim + role claim, deny by default, **tests in CI with the emulator**.
2. Set `company_id` / `role` custom claims from the Go backend at login or role change.
3. In Go, treat the Admin SDK like a superuser connection: check permissions in code.
4. Keep the Firebase service-account key in OpenBao, not in `.env`.
