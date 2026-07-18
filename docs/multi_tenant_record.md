# Multi-Tenant Isolation — Full Project Record

Structured record of the complete multi-tenant isolation effort for
new-api: requirements, database changes, implementation, the bug found
during verification, test coverage, git history, remote verification
results, and operational precautions. Companion documents:
[`multi_tenant_implementation_plan.md`](./multi_tenant_implementation_plan.md)
(the approved design spec) and
[`multi_tenant_release_summary.md`](./multi_tenant_release_summary.md)
(the phase-by-phase completion summary).

---

## 1. Requirements

new-api had no tenant/organization concept prior to this work. `Role` was
a flat int (Guest/Common/Admin/Root) and `Group` (on User/Token/Channel)
was purely a pricing/routing tag — any user could be assigned any group,
channels were global system resources with no ownership field, and admin
was a binary global gate.

Requirements, as they were established over the course of this work:

1. **Full multi-tenant isolation**: separate organizations ("tenants"),
   each with their own users, channels, tokens, and logs.
2. **Channel sharing model**: channels support both a shared/global pool
   (`tenant_id = 0`, usable by every tenant) and tenant-private channels
   (`tenant_id = N`).
3. **Role semantics**: `RoleRootUser` stays cross-tenant everywhere.
   `RoleAdminUser` becomes tenant-scoped — full admin rights, but confined
   to their own tenant's users/tokens/logs — and, per explicit
   confirmation, may also manage *shared* channels in addition to their
   own tenant's private ones.
4. **Tenant assignment is root-only**: a tenant-scoped Admin's
   create/update requests are always force-assigned to their own tenant
   server-side, regardless of what the request body claims.
5. **No new trusted client input**: the frontend never sends a
   "current tenant" header the backend trusts. Tenant scope is always
   resolved server-side from the session; the only client-supplied
   override is Root's `?tenant_id=` query param on list endpoints.
6. **Backward compatibility**: every existing single-tenant deployment
   must upgrade with zero manual steps — `tenant_id = 0` is the default
   everywhere, meaning "no tenant / shared," so existing rows behave
   identically post-upgrade.
7. **Full-stack**: backend (Go) and frontend (`web/default`, React)
   both required — a Tenant management page, tenant pickers on
   user/channel forms, and a tenant filter on the logs view.
8. **Verification**: local (SQLite fresh-boot + realistic
   upgrade-from-existing-DB test) and, per explicit later instruction,
   live verification against the remote PostgreSQL deployment at
   `47.115.216.176:5432`.

---

## 2. Database Changes

All changes are **additive only** — no columns dropped, no types changed,
no data rewritten. This was a deliberate design principle so that
upgrading a live database is safe and requires no manual migration steps.

### New table

```
tenants
  id            (PK, autoincrement — id 0 is a reserved sentinel meaning
                 "Shared / Global" and is never a real row)
  name          varchar(64), unique, not null
  status        int, default 1 (TenantStatusEnabled=1 / Disabled=2)
  remark        varchar(255)
  created_time  bigint
  updated_time  bigint
  deleted_at    (soft delete, standard GORM DeletedAt)
```

### New column: `tenant_id int, default 0, indexed`

Added to five existing tables, always with the same semantics
(`0` = shared/no tenant):

| Table | Notes |
|---|---|
| `users` | The tenant a user belongs to. |
| `channels` | `0` = shared/global channel, usable by all tenants. `N` = private to tenant N. |
| `tokens` | Denormalized from the owning user at token-creation time. |
| `logs` | Denormalized from the acting user at log-write time. |
| `abilities` | Denormalized from the owning channel's `tenant_id`, written by `AddAbilities`/`UpdateAbilities`. |

### Migration mechanism

Registered in the existing GORM `AutoMigrate` call in `model/main.go`
(`migrateDB()`, invoked on every boot when `common.IsMasterNode` is true)
— the same path every other schema change in this codebase goes through.
No hand-rolled `HasColumn`-guarded migration was needed, since a new int
column is fully supported by `AutoMigrate` on SQLite, MySQL, and
PostgreSQL alike (unlike column-*type* changes, which this codebase does
guard by hand elsewhere).

### Backward compatibility verified

Two independent tests confirmed zero data loss on upgrade:

- **Local SQLite**: built a binary from the pre-tenant code, booted it to
  create a database with a real user row, then booted the tenant-aware
  binary against a copy of that database. Migration completed with zero
  errors; the pre-existing user survived intact with `tenant_id`
  defaulted to `0`.
- **Remote PostgreSQL** (see §6): same result against the live remote
  instance — pre-existing `admin` user and 4 log rows unchanged after
  migration.

---

## 3. Implementation Summary

| Phase | Scope |
|---|---|
| A | `Tenant` model + CRUD, migration, `tenant_id` columns |
| B | Session/token context plumbing (`EffectiveTenantId`, `AssertSameTenant` helpers in `middleware/auth.go`) |
| C | Channel/ability selection scoping — DB-fallback path (`model/ability.go`), memory-cache path (`model/channel_cache.go`), and the request distributor (`middleware/distributor.go`), including the explicit-channel-id and affinity-reuse paths |
| D | Admin query scoping (`GetAllUsers`/`SearchUsers`, `GetAllChannels`/`SearchChannels`/`SearchTags`, `GetAllLogs`) + tenant-assignment write paths (`CreateUser`, `AddChannel`, `CopyChannel`, `UpdateUser`, `UpdateChannel`) |
| E | Root-only Tenant CRUD API (`controller/tenant.go`, `/api/tenant`, gated by `middleware.RootAuth()`) |
| F | Frontend: Tenant management page (`web/default/src/features/tenants/`), tenant pickers on user/channel drawers, tenant filter on the logs view, sidebar entry gated to `ROLE.SUPER_ADMIN` |

Key design decisions:

- Tenant scoping is a **data-layer concern layered under existing RBAC** —
  `authz.Can`/Casbin permissions decide "can this role touch this
  resource type"; tenant filtering is `WHERE tenant_id IN (...)` added to
  `model/*.go` query functions, the same shape as the pre-existing
  `Group` filters.
- `Ability` denormalizes `tenant_id` from its owning `Channel`, exactly
  like it already denormalizes `Group`/`Priority`/`Weight`/`Tag` — keeps
  the hot-path channel-selection queries join-free.
- Single-resource operations (`GetChannel`/`UpdateChannel`/`DeleteChannel`/
  `TestChannel`/`CopyChannel`) call `AssertSameTenant(c, channel.TenantId, allowShared=true)` — Root bypasses; a tenant-scoped Admin may act on
  their own tenant's channels **and** shared channels, per the confirmed
  policy.

---

## 4. Bug Found and Fixed: `UpdateUser` Tenant Persistence

Found during live browser-driven verification (§6), not by any unit test
written beforehand — this is the reason end-to-end verification mattered.

**Symptom**: assigning a user to a tenant via the Update-User drawer
showed "User updated successfully," but the database still showed
`tenant_id = 0` afterward.

**Root cause**: in `controller/user.go`, `UpdateUser` calls
`updatedUser.EditWithTx(tx, updatePassword)`, whose last line is
`tx.First(user, user.Id)` — a refetch that overwrites the entire
in-memory `updatedUser` struct (including `TenantId`) with the **current
database row**. At the time of this refetch, `tenant_id` in the database
is still the old value, because the actual reassignment write
(`updateUserTenantForUserInTx`) hadn't run yet. The reassignment code then
read `updatedUser.TenantId` — which had just been silently reset to the
old value — compared it to itself, decided "unchanged," and took the
no-op path. The handler still returned `success: true` because no error
occurred anywhere in the transaction.

**Fix**: capture the client-requested tenant id into a local variable
(`requestedTenantId := updatedUser.TenantId`) **before** `EditWithTx`
runs, and pass that captured value to `updateUserTenantForUserInTx`
instead of re-reading the (by-then-clobbered) struct field.

```go
// Captured before EditWithTx runs: EditWithTx's trailing refetch
// (tx.First(user, user.Id)) overwrites *updatedUser from the DB, which
// would clobber the client-requested tenant_id back to its pre-update
// value before updateUserTenantForUserInTx ever reads it.
requestedTenantId := updatedUser.TenantId
```

**Verification of the fix**: re-ran the exact same UI flow (Playwright,
real authenticated session) against the rebuilt binary — the database
correctly showed `tenant_id = 1` after the update. A regression test,
`TestUpdateUser_RootReassignsTenant_ActuallyPersists`
(`controller/tenant_scoping_test.go`), was added; it fails against the
pre-fix code and passes against the fix.

---

## 5. Test Cases

21 new top-level Go test functions across 8 new test files (several with
multiple `t.Run` subtests), plus one live end-to-end verification pass
through the real running application (backend + frontend + browser).

| File | Test functions | What it covers |
|---|---|---|
| `middleware/auth_test.go` | 2 | `EffectiveTenantId` resolution (session vs. token path, explicit-zero vs. unset); `AssertSameTenant`'s full decision matrix (Root / same-tenant Admin / other-tenant Admin / shared resource with `allowShared` true and false) |
| `middleware/distributor_test.go` | 3 | Explicit-channel-id routing: cross-tenant request → 403; same-tenant → allowed; shared channel → allowed for any tenant |
| `model/ability_tenant_test.go` | 1 (3 subtests) | DB-fallback `GetChannel`: private channel only reachable by its own tenant; shared channel reachable by every tenant; foreign tenant never sees another tenant's private channel even with a shared one present |
| `model/channel_cache_tenant_test.go` | 1 (2 subtests) | Same matrix through the memory-cache path (`InitChannelCache` + `GetRandomSatisfiedChannel`) |
| `model/tenant_scoping_test.go` | 5 | `GetAllUsers`/`SearchUsers` tenant filter (including the `tenant_id=0` "meaningful value, not no-filter" case); `SearchChannels`/`ApplyChannelTenantFilter` shared+own semantics; `GetAllLogs` tenant filter |
| `model/tenant_test.go` | 4 | Tenant insert/get; `id=0` sentinel rejected by `GetTenantById`; name-duplicate detection; `DeleteTenantById` blocked while a user or channel still references the tenant |
| `controller/tenant_test.go` | 2 | Root CRUD round-trip (create → duplicate-name rejected → update → list → delete) through the actual HTTP handlers; delete-blocked-by-references returns an error response |
| `controller/tenant_scoping_test.go` | 3 | `CreateUser` force-assigns a tenant-scoped Admin's own tenant even when the request claims a different one; Root may assign any tenant on create; **regression test for the `UpdateUser` persistence bug** (§4) |

**Live end-to-end verification** (browser-driven, not a unit test — see
§6 for the remote-database portion and the session transcript for the
local portion): tenant CRUD through the real UI, tenant assignment
through the real UI (where the bug above was caught), sidebar/route
gating for non-root admins, user-list and channel-list scoping through
the real UI, and four direct API probes (GET/PUT/DELETE on a foreign
tenant's channel, PUT on a shared channel) confirming the same
authorization boundaries at the HTTP layer.

All of `go build ./...` and `go test ./...` (full repository) pass clean
as of the commit in §6. `bun run typecheck` and `bun run build`
(production frontend bundle) also pass clean.

---

## 6. Online (Remote) Verification Conclusions

Performed against the live remote PostgreSQL instance at
`47.115.216.176:5432` (database `newapi`), per an explicit, standalone
instruction dedicated solely to this action (see §8 for why this required
special handling).

**Pre-flight read-only inspection** (before any write): the remote
database held exactly 1 user (`admin`, role Root), 0 channels, 0 tokens,
4 log rows — consistent with a near-empty deployment stood up for this
exercise, not a live production system with real traffic.

**Steps performed, in order:**

1. **Migration** — ran the real, unmodified `model.InitDB()` migration
   path (the same code every deployment runs on boot) against the remote
   DSN via a standalone Go program, without booting the full application
   server (which would also start background jobs like dashboard-data
   writes and credential-refresh tasks — out of scope for a migration
   check). Result: `tenants` table created; `tenant_id` columns added to
   users/channels/tokens/logs/abilities, purely via `ALTER TABLE ... ADD
   COLUMN` / `CREATE TABLE`. The pre-existing user and log rows were
   verified untouched before and after.
2. **Isolation verification** — created a temporary real `Tenant` row
   plus a shared and a tenant-private test channel, then exercised the
   actual `model.GetChannel` (DB-fallback path) and
   `model.GetRandomSatisfiedChannel` (memory-cache path) functions
   against the live instance:
   - Foreign tenant only ever resolved the shared channel (15/15 draws).
   - Owning tenant could reach both its private and the shared channel.
   - `tenantId = 0` only ever resolved the shared channel.
   - `DeleteTenantById` correctly blocked while a channel still
     referenced the tenant.
   - All 8 checks passed.
3. **Cleanup** — all test rows (channel, ability, tenant) were deleted
   afterward, including hard-deleting the soft-deleted `Tenant` row so no
   trace remains. A final inspection confirmed `channels`/`abilities`/
   `tenants` row counts returned to pre-test levels and the original
   `admin` user and 4 log rows were unchanged.

**Conclusion**: schema migrated successfully on real PostgreSQL (not just
SQLite), the tenant-isolation logic behaves identically on the actual
production database engine as it does under the local Go test suite, and
the remote database was left in a clean state — new schema, zero data
loss, zero leftover test data.

---

## 7. Git Commit Record

| | |
|---|---|
| Commit | `7279e366f0bf6b4828faf35ed00d1e94d185c450` |
| Branch | `v1.0.0-rc.21` |
| Parent | `bde9b2f44887d34ec54799ae191d50f97914359e` |
| Author | ugghub99 \<ugghub99@gmail.com\> |
| Co-author trailer | Claude Sonnet 5 \<noreply@anthropic.com\> |
| Stat | 66 files changed, 3119 insertions(+), 529 deletions(-) |
| Remote | `git@github.com:ugghub99/new-api-cp.git` |
| Push result | `bde9b2f4..7279e366  v1.0.0-rc.21 -> v1.0.0-rc.21` (fast-forward, no force) |

Commit message:

```
feat: add multi-tenant isolation across backend and frontend

Adds a full Tenant concept so separate organizations can share one
deployment: each tenant gets its own users, private channels, tokens,
and logs, plus a shared/global resource pool usable by every tenant.

- New Tenant model + migration; tenant_id added to User/Channel/Token/
  Log/Ability, defaulting to 0 ("shared/no tenant") so existing
  single-tenant deployments upgrade with zero behavior change.
- Channel/ability selection (DB and memory-cache paths) and the
  request distributor enforce tenant boundaries on every routing
  decision, including explicit-channel-id and affinity-reuse paths.
- Admin list/query endpoints (users, channels, logs) scope to the
  caller's tenant; Root can filter by any tenant or see all. Root-only
  tenant CRUD API at /api/tenant. Tenant-scoped Admins are always
  force-assigned their own tenant on create, regardless of request
  body, and can additionally manage shared channels.
- Frontend: Root-only Tenant Management page, tenant pickers on the
  user/channel create-edit drawers, a tenant filter on the logs view,
  and role-based hiding of all of the above for non-root admins.
- Fixes a bug found during end-to-end verification: UpdateUser's
  EditWithTx refetch was clobbering the in-memory tenant_id before the
  reassignment write read it, making tenant reassignment silently a
  no-op while still reporting success. Added a regression test.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

Deliberately excluded from the commit: 5 untracked scratch files
(`web/default/src/i18n/locales/_reports/*.untranslated.json`) — these are
regenerable `bun run i18n:sync` byproducts and don't match this repo's
existing tracked pattern (only `_sync-report.json` itself is tracked).
`.env` was never staged (confirmed gitignored; confirmed no secrets in
the staged diff).

---

## 8. Precautions

### `.env` / credentials

- The repository's local `.env` points `SQL_DSN` at the remote host used
  for verification in §6, with a **plaintext password** in the file.
  This was flagged at the start of this work and again at the point of
  remote verification. **Recommend rotating this credential** and moving
  it to a secret manager or environment-injected secret rather than a
  plaintext `.env` file, regardless of anything else in this record —
  the password was incidentally visible throughout this session.
- No secret was committed to git at any point (`.env` is gitignored; the
  staged diff for the commit in §7 was checked and contains no
  credentials).
- If this deployment is genuinely used beyond throwaway
  testing/verification, treat the current password as **potentially
  exposed** (it passed through a local, non-production `.env` file and
  multiple ad hoc verification scripts) and rotate it before relying on
  it for anything sensitive.

### Applying this migration to a real production database

This migration is additive-only (`CREATE TABLE`, `ALTER TABLE ... ADD
COLUMN`) and was verified safe on both SQLite and live PostgreSQL in this
session — but the verification database was near-empty. Before running
this against a **large, active production database**, consider:

- **Table lock duration on `ADD COLUMN`.** PostgreSQL ≥ 11 and MySQL ≥
  8.0.12 can add a nullable/defaulted column without a full table
  rewrite in most cases, but always confirm on the actual target
  version — MySQL 5.7 (the minimum this codebase claims to support, per
  `AGENTS.md`) does **not** have this optimization for all storage
  engines and may briefly lock large tables (`logs` in particular, which
  is typically the largest table in this schema) during the `ALTER
  TABLE`. Test against a copy of production-scale data first, or run
  during a low-traffic window.
- **Master-node-only execution.** `model/main.go`'s migration path is
  already gated by `common.IsMasterNode` (`NODE_TYPE=slave` skips
  migration). In a multi-node/multi-instance production deployment,
  ensure only one node runs the migration and that other nodes don't
  start serving traffic against the new schema before that migration
  completes — this codebase's existing convention handles the "don't run
  migration from every node" half of that; the "wait for schema before
  serving" half is an operational/deployment-orchestration concern, not
  something this change adds automated handling for.
- **No physical sharding is introduced by this work.** "Multi-tenant"
  here means *logical* isolation — one shared database, tenant boundaries
  enforced by `tenant_id` filtering in application queries. If a future
  requirement is *physical* sharding (separate database instances or
  schemas per tenant, or per group of tenants, for scale/compliance
  reasons), that is a substantially larger, separate migration: it would
  need a tenant→shard routing layer, per-shard connection pooling, and
  either fan-out queries or a ban on cross-tenant queries at the
  infrastructure level (today's shared/global channel pool, which
  deliberately allows cross-tenant visibility for `tenant_id=0`
  resources, would need explicit reconciliation with any sharding
  design, since shared resources by definition cross shard boundaries).
  Nothing in the current schema or code assumes or blocks a future move
  to physical sharding, but nothing does that work either.
- **Rollback.** Because every change is additive, rolling back the
  *application code* to the pre-tenant version is safe even after the
  migration has run — old code simply never reads the new `tenant_id`
  columns or `tenants` table. Rolling back the *schema* itself
  (dropping the new column/table) is not necessary for a code rollback
  and is not recommended once any tenant data has been written, since it
  would be a destructive, non-additive operation outside the safety
  guarantees this work was designed around.
- **Test on a copy first.** Regardless of the above, the standing
  guidance from this session's plan holds: verify against a
  representative copy of the production database (schema *and* realistic
  row counts) before running against the live system, the same way the
  §6 verification used read-only inspection before any write.

### Known follow-up gaps (not addressed in this work)

- `DeleteDisabledChannel` (bulk endpoint) and `GetLogsStat`/usage
  aggregates still operate cross-tenant; a tenant-scoped Admin can
  trigger/see fleet-wide effects there.
- Local Postgres/MySQL **container** smoke tests (as distinct from the
  live remote verification in §6) were not run — explicitly skipped by
  instruction in favor of the remote verification.
- No live browser walkthrough was performed against the remote
  deployment itself — all browser-driven verification (§5's
  end-to-end pass) ran against a local instance; only the migration and
  model-layer isolation checks (§6) ran against the remote database
  directly.
