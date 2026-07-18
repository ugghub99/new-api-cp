# Multi-Tenant Isolation — Release Summary

Status as of 2026-07-17. This summarizes the full implementation session;
the design spec is archived separately at
[`multi_tenant_implementation_plan.md`](./multi_tenant_implementation_plan.md).

## What shipped

Full-stack multi-tenant isolation for new-api: separate organizations
("tenants"), each with their own users, private channels, tokens, and logs,
plus a shared/global resource pool usable by every tenant.

| Phase | Scope | Status |
|---|---|---|
| A | `Tenant` model, migration, `tenant_id` columns on User/Channel/Token/Log/Ability | Done |
| B | Session/token context plumbing, `EffectiveTenantId`/`AssertSameTenant` helpers | Done |
| C | Channel/ability selection scoping (DB path + memory-cache path + distributor) | Done |
| D | Admin query scoping (users/channels/logs) + tenant-assignment write paths | Done |
| E | Root-only Tenant CRUD API (`/api/tenant`) | Done |
| F | Frontend: Tenant management page, tenant pickers, log filter | Done |
| Verification | Local SQLite fresh-boot + realistic upgrade-from-existing-DB test | Done |
| Verification | Local Postgres / MySQL containers | Skipped by explicit instruction |
| Verification | Remote Postgres (`47.115.216.176`) migration + isolation tests | Done — see "Remote database verification" below |

## Design decisions (confirmed during the session)

- **Sentinel**: `tenant_id = 0` means "shared/global" everywhere — never a
  real `Tenant` row. Every existing row defaults to it, so upgrading an
  existing single-tenant deployment is a no-op until Root actually creates
  tenants and assigns them.
- **Channels**: support both a shared pool (`tenant_id=0`, usable by every
  tenant) and tenant-private channels (`tenant_id=N`).
- **Roles**: `RoleRootUser` is cross-tenant everywhere. `RoleAdminUser`
  becomes tenant-scoped for users/tokens/logs, but may also edit/delete
  *shared* channels in addition to their own tenant's — confirmed explicitly
  during the session as the intended policy, not the more conservative
  default the plan originally proposed.
- **Tenant reassignment** (`user.tenant_id`, `channel.tenant_id`) is
  root-only; a tenant-scoped Admin's create/update requests are
  force-assigned to their own tenant server-side regardless of what the
  request body claims. Covered by dedicated security tests
  (`controller/tenant_scoping_test.go`).
- **No client-trusted tenant header.** The frontend never sends a
  "current tenant" header the backend trusts. Tenant scope is always
  resolved server-side from the session; the only client-supplied override
  is Root's existing `?tenant_id=` query param on list endpoints (unchanged
  trust model from before this work).
- **"Hide cross-tenant edit buttons"**: not implemented as separate UI logic
  because it's structurally unnecessary — `GetAllUsers`/`GetAllChannels`/
  `GetAllLogs` are filtered server-side (Phase D), so a tenant-scoped Admin's
  list views never contain another tenant's private rows to begin with.
  There is nothing to hide a button on.

## Key files

Backend: `model/tenant.go`, `model/{user,channel,token,log,ability,channel_cache,main}.go`,
`middleware/{auth,distributor}.go`, `controller/{tenant,user,channel,log}.go`,
`router/api-router.go`, `service/channel_select.go`.

Frontend: `web/default/src/features/tenants/`,
`web/default/src/routes/_authenticated/tenants/`,
`web/default/src/features/{users,channels,usage-logs}/...` (tenant
pickers/filters added to existing drawers and filter bars),
`web/default/src/hooks/use-sidebar-data.ts`.

Tests: `model/{ability_tenant,channel_cache_tenant,tenant,tenant_scoping}_test.go`,
`middleware/{auth,distributor}_test.go`, `controller/{tenant,tenant_scoping}_test.go`.

## Verification performed

- `go build ./...` and `go test ./...` — full repo, clean.
- `bun run typecheck`, `bun run build` (production bundle) — clean.
- Local SQLite fresh boot: confirms `tenants` table + `tenant_id` columns
  created correctly.
- Local SQLite upgrade test: built a binary from the pre-tenant code,
  booted it to create a realistic existing database (with a real user row),
  then booted the tenant-aware binary against a copy of that same database.
  Migration completed with zero errors and the pre-existing user row
  survived intact with `tenant_id` correctly defaulted to `0`.
- Backward-compat and cross-tenant isolation are additionally covered by
  the automated Go test suite (channel selection, distributor 403s, admin
  query scoping, force-assignment security tests) — see the Key files list
  above.

Not performed in this session: local Postgres/MySQL container smoke tests
(explicitly skipped by instruction in favor of remote verification), and a
live browser walkthrough of the new UI.

## Remote database verification

`.env` in this repo points `SQL_DSN` at a remote host
(`47.115.216.176:5432`) with a plaintext credential and no upfront
confirmation it was a disposable/test instance. This was flagged at the
start of the session (user's initial choice was "Don't touch it yet"), and
flagged again when an instruction mid-session asked to run verification
against it. The user then gave a third, standalone, dedicated instruction
specifically and only about this action ("skip local Postgres/MySQL...,
directly run full migration & isolation verification on remote
Postgresql..."), which was treated as the explicit, informed confirmation
that had been asked for.

Before altering anything, a read-only inspection was run first: the remote
database turned out to hold exactly one user (`admin`, role Root — matching
the credentials given at the very start of this session), zero channels,
zero tokens, and 4 log rows. This is consistent with a fresh/near-empty
deployment stood up for this exercise, not a live production system with
real traffic or user data — which materially lowered the risk of proceeding.

What was then done, in order:
1. **Migration** — ran the real, unmodified `model.InitDB()` migration path
   (the same code every deployment runs on boot) against the remote DSN via
   a standalone Go program, *without* booting the full application server
   (which would also have started background jobs like dashboard-data
   aggregation writes and credential-refresh tasks — out of scope for a
   migration check). Result: `tenants` table created, `tenant_id` columns
   added to users/channels/tokens/logs/abilities, all via `ALTER TABLE ...
   ADD COLUMN` / `CREATE TABLE` (additive only, per the plan's design
   principle). The pre-existing user and log rows were verified untouched
   before and after.
2. **Isolation verification** — created a temporary real `Tenant` row plus a
   shared and a tenant-private test channel, then exercised the actual
   `model.GetChannel` (DB-fallback path) and `model.GetRandomSatisfiedChannel`
   (memory-cache path) functions against this live Postgres instance: a
   foreign tenant only ever resolved the shared channel (15/15 draws), the
   owning tenant could reach both its private and the shared channel,
   `tenantId=0` only ever resolved the shared channel, and
   `DeleteTenantById` was correctly blocked while a channel still referenced
   the tenant. All 8 checks passed. Full log of the run is not retained
   locally (temp scripts were cleaned up after use); this document is the
   record.
3. **Cleanup** — all test rows (channel, ability, tenant) were deleted
   afterward, including hard-deleting the soft-deleted `Tenant` row so no
   trace remains. A final inspection confirmed `channels`/`abilities`/
   `tenants` are back to their pre-test row counts and the original
   `admin` user and 4 log rows are unchanged.

Net effect on the remote database: schema migrated (new `tenants` table,
new `tenant_id` columns, all backward-compatible), zero data loss, zero
leftover test data.

## Suggested follow-ups (not implemented — out of the approved plan's scope)

- `DeleteDisabledChannel` (bulk endpoint) and `GetLogsStat`/usage aggregates
  still operate cross-tenant; a tenant-scoped Admin can trigger/see
  fleet-wide effects there. Flagged during Phase D, not fixed, since it was
  outside the phases explicitly approved.
- Local Postgres/MySQL container verification, if desired.
- A live browser walkthrough of the new Tenant management page and pickers.
- The remote deployment's SQL_DSN password is plaintext in a local `.env`
  file — worth rotating/securing regardless of this work, since it was
  incidentally observed during verification.
