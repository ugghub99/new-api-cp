# Multi-Tenant Isolation for new-api

## Context

new-api currently has no tenant/organization concept. `Role` is a flat int
(`RoleGuestUser=0`, `RoleCommonUser=1`, `RoleAdminUser=10`, `RoleRootUser=100`,
`common/constants.go`) and `Group` (on `User`/`Token`/`Channel`, and the
`Ability{Group,Model,ChannelId}` composite key) is purely a pricing/routing
tag — any user can be assigned any group, channels are global system
resources with zero ownership field, and admin is a binary global gate
(`middleware/auth.go` `AdminAuth()`/`RootAuth()`). Confirmed via direct
reads of `model/user.go`, `model/channel.go`, `model/ability.go`,
`middleware/auth.go` — a grep for `OwnerId|TenantId|OrganizationId|OrgId`
across `model/`, `middleware/`, `controller/`, `service/` returns zero hits.

Goal: add full multi-tenant isolation — separate organizations, each with
their own users/channels/tokens/logs, full-stack (backend + web/default
UI). Decided scope:
- Channels support both a **shared/global pool** (`tenant_id = 0`, usable by
  every tenant) and **tenant-private channels** (`tenant_id = N`).
- Only `RoleRootUser` creates/deletes tenants and reassigns a user's
  `tenant_id`. `RoleAdminUser` becomes tenant-scoped: full admin rights,
  but confined to their own tenant's users/channels/tokens/logs — **and**,
  per explicit confirmation, tenant-scoped Admins may also edit/delete
  shared (`tenant_id=0`) channels, not just their own tenant's private ones.
- This must be done entirely in code first (GORM models + AutoMigrate,
  matching this repo's existing migration conventions in `model/main.go`)
  and verified locally against SQLite/local Postgres/local MySQL — the
  `.env`'s remote Postgres host is never touched during this work.
- Every existing single-tenant deployment must upgrade with zero manual
  steps: `tenant_id = 0` is the default (Go zero value) everywhere, meaning
  "no tenant / shared" — existing rows behave identically post-upgrade.

## Design principles

1. **Additive, not invasive.** New `tenant_id int` columns default to `0`
   via GORM's normal zero-value insert — no `gorm:"default:..."` tag needed
   (avoids the MySQL/Postgres boolean-default ALTER-TABLE churn AGENTS.md
   warns about; irrelevant here since it's an int, but same principle: let
   the zero value do the work).
2. **Tenant scoping is a data-layer concern layered under existing RBAC.**
   `RequirePermission`/`authz.Can` (`service/authz/permission.go`) decides
   "can this role touch this resource type" — no row-level notion, and it
   stays that way. Tenant filtering is `WHERE tenant_id IN (...)` added to
   `model/*.go` query functions, the same shape as the existing `Group`
   filters (`ApplyChannelGroupFilter`, `logGroupCol` `WHERE` clauses).
3. `RoleRootUser` (100) is cross-tenant everywhere. `RoleAdminUser` (10) is
   forced into their own tenant for **users/tokens/logs**, but may act on
   `tenant_id IN (0, ownTenant)` for **channels** (shared channels included,
   per the confirmed policy). `RoleCommonUser`/`Guest` are unaffected — they
   are already scoped to themselves via `UserId`.
4. **Channel sharing:** `Channel.TenantId == 0` → visible/selectable by
   every tenant. `Channel.TenantId == N` → only tenant `N`. `Ability`
   denormalizes `TenantId` from its owning `Channel`, exactly like it
   already denormalizes `Group`/`Priority`/`Weight`/`Tag` — this keeps the
   hot-path channel-selection queries join-free.

## Phase A — Data model + backward-compatible migration

Land schema changes that are inert until later phases read/write them.
Independently mergeable/testable.

- New `model/tenant.go`: `Tenant{Id int, Name string (unique, varchar(64)),
  Status int, Remark string, CreatedTime int64, UpdatedTime int64}`. Add
  `TenantStatusEnabled`/`TenantStatusDisabled` constants next to
  `RoleRootUser` in `common/constants.go`. `Id == 0` is a reserved sentinel
  ("Shared / Global") — never insert a real row with `Id = 0`; standard
  autoincrement (SQLite/Postgres/MySQL all start at 1) handles this for
  free. Add basic CRUD: `CreateTenant`, `GetTenantById`, `GetAllTenants`,
  `UpdateTenant`, `DeleteTenant` (soft delete via `Status`; hard-block
  deletion if any `User`/`Channel` still references the tenant, to avoid
  orphaned FKs).
- Add `TenantId int` (`gorm:"type:int;default:0;index"`) to `User`
  (`model/user.go`, after `Role`), `Channel` (`model/channel.go`, after
  `Group`), `Token` (`model/token.go`, after `Group` — denormalized from
  the owning user at creation, no new list-scoping needed this pass since
  `GetAllUserTokens`/`SearchUserTokens` are already always scoped by
  `userId`), `Log` (`model/log.go`, after `Group` — denormalized from the
  acting user at write time, same pattern as the existing `Group` column),
  and `Ability` (`model/ability.go`, after `ChannelId` — denormalized from
  owning `Channel.TenantId`, written by `AddAbilities`/`UpdateAbilities`
  just like `Priority`/`Weight`/`Tag` already are).
- Register `&Tenant{}` in **both** `DB.AutoMigrate(...)` (`migrateDB()`,
  `model/main.go:271-302`) and the `migrations` slice in `migrateDBFast()`
  (`model/main.go:318-354`) — `migrateDB()` is the one actually invoked at
  boot (`model/main.go:214`). No hand-rolled `HasColumn`-guarded migration
  needed for the new int columns — this is the same simple AutoMigrate path
  already used for every other plain new column in this codebase; the
  `ensure*TableSQLite` hand-migrations exist only for column-*type* changes
  GORM's SQLite driver can't ALTER in place, which doesn't apply here.
- `model/user_cache.go`: add `TenantId int` to `UserBase`, populate it in
  `User.ToBaseUser()` (`model/user.go:58-70`), and set
  `constant.ContextKeyUserTenantId` (new key, added next to
  `ContextKeyUserGroup` in `constant/context_key.go`) inside
  `(*UserBase).WriteContext`. No incremental per-field cache update needed
  — the whole `UserBase` is cached as one object and tenant reassignment
  already goes through `model.InvalidateUserCache`.

**Verify:** `go build ./...`; `go test ./model/...`; boot fresh against
SQLite and a local Postgres container, confirm the `tenants` table and new
columns exist; boot against a pre-migration SQLite fixture and confirm
existing rows get `tenant_id=0` with no behavior change (nothing reads the
column yet).

## Phase B — Request-context plumbing + tenant-scope helpers

- `controller/user.go` `setupLogin`: `session.Set("tenant_id",
  user.TenantId)` alongside the existing `session.Set("group", ...)`.
- `middleware/auth.go` `authHelper`: after the existing
  `c.Set("group", session.Get("group"))`, add
  `c.Set("tenant_id", session.Get("tenant_id"))`. Apply the same treatment
  to the `ValidateAccessToken` fallback branch.
- Token-based relay auth: `SetupContextForToken` already calls
  `userCache.WriteContext(c)`, which sets `ContextKeyUserTenantId` once
  Phase A's `UserBase`/`WriteContext` change lands — no extra code.
- Add next to `AdminAuth()`/`RootAuth()` in `middleware/auth.go`:
  - `EffectiveTenantId(c *gin.Context) int` — single source of truth,
    reads `c.GetInt("tenant_id")` / `ContextKeyUserTenantId`.
  - `AssertSameTenant(c *gin.Context, resourceTenantId int, allowShared bool) bool`
    — `role >= RoleRootUser` → true; else `resourceTenantId ==
    EffectiveTenantId(c)`, OR `resourceTenantId == 0 && allowShared` (used
    for channels, per the confirmed policy that tenant-admins may also
    touch shared channels; users/tokens/logs call this with
    `allowShared=false` since those have no "shared" concept). This is a
    controller-level inline check (mirrors the existing
    `canManageTargetRole` pattern in `controller/user.go`), not a static
    route-level `.Use()`, since it needs the specific resource's tenant id
    from a DB lookup.

**Verify:** new/extended `middleware/auth_test.go` covering
`EffectiveTenantId` for session vs token paths, and `AssertSameTenant`'s
decision matrix (Root / same-tenant Admin / other-tenant Admin / shared
resource with `allowShared` true and false).

## Phase C — Channel/Ability selection scoping (core routing change)

Highest-risk phase — changes hot-path request routing. Land with full test
coverage; behavior is a no-op for existing deployments since every row is
`tenant_id=0` until Phase D lets Root actually assign tenants.

- `model/ability.go`: add `tenantId int` to `getPriority`, `getChannelQuery`,
  `GetChannel` — extend the `Where` clause with
  `"and (tenant_id = 0 or tenant_id = ?)"`. When `tenantId==0` this
  collapses correctly to shared-only.
- `model/channel_cache.go` (memory-cache hot path): `GetRandomSatisfiedChannel`
  gains `tenantId int`; `filterChannelsByRequestPathAndModel` gets an added
  tenant filter pass using the already-loaded `channelsIDM[id].TenantId`
  (no changes to `InitChannelCache`'s cache-build map — the tenant check is
  applied at read time). `CacheGetChannel`/`CacheGetChannelInfo`
  (single-channel-by-id lookups) do **not** take a tenant parameter — every
  caller that resolves a specific channel by ID and uses it to serve a
  request must independently verify
  `channel.TenantId in (0, callerTenantId)`, mirroring how `channel.Status`
  is already independently re-checked at call sites rather than baked into
  the cache lookup.
- `service/channel_select.go`: add `TenantId int` to `RetryParam`; thread it
  into every `model.GetRandomSatisfiedChannel(...)` call in
  `CacheGetRandomSatisfiedChannel`.
- `middleware/distributor.go` `Distribute()`: resolve
  `tenantId := EffectiveTenantId(c)` at the top. The explicit-channel-ID
  path (`ContextKeyTokenSpecificChannelId`) — after the existing
  `channel.Status` check, add a tenant check
  (`channel.TenantId != 0 && channel.TenantId != tenantId` → 403). The
  affinity-reuse path (`GetPreferredChannelByAffinity`) needs the same
  independent tenant check before marking the preferred channel usable.
  Add `TenantId: tenantId` to the `RetryParam{...}` literal.
- `model/ability.go` listing functions used for model-catalog/pricing
  display (`GetGroupEnabledModels`, `GetAllEnableAbilityWithChannels`) gain
  an optional `tenantId int` (0 = shared-only, matching an anonymous
  caller) so a tenant's private models don't leak into other tenants'
  "available models" listings — grep call sites across `controller/`,
  `service/`, `setting/` and update each.

**Verify (new tests, testify `require`/`assert`, in-memory SQLite fixtures
matching `model/pricing_endpoint_test.go`'s style):**
- `model/ability_tenant_test.go`: two channels (shared + tenant-7 private)
  offering the same model/group; assert `GetChannel(...,tenantId=7)` can
  return either, `tenantId=9` and `tenantId=0` only ever return the shared
  one.
- `model/channel_cache_tenant_test.go`: same matrix through
  `InitChannelCache()` + `GetRandomSatisfiedChannel` with
  `common.MemoryCacheEnabled=true`, deterministic weights so the private
  channel is provably never returned for a foreign/zero tenant.
- `middleware/distributor_test.go`: mocked context with `tenant_id=7`
  requesting another tenant's private channel via
  `ContextKeyTokenSpecificChannelId` → assert 403.
- Full `go test ./model/... ./service/... ./middleware/...` stays green
  (every existing row is `tenant_id=0`, so behavior must be unchanged).

## Phase D — Admin query scoping + tenant-assignment write paths

- **Logs** (`model/log.go`): add `tenantId int` to `GetAllLogs`/
  `GetUserLogs`, same `if tenantId != 0 { tx = tx.Where("logs.tenant_id = ?", tenantId) }`
  shape as the existing `group` filter. `controller/log.go`: Admin is
  forced to `EffectiveTenantId(c)` (ignore client-supplied filter); Root
  gets an optional `?tenant_id=` query param (absent/0 = all tenants).
  `GetUserLogs` (self-scoped by `userId`) needs no change.
- **Users** (`model/user.go`): add `tenantId *int` to `GetAllUsers`/
  `SearchUsers` (pointer so "not provided" ≠ the meaningful value `0`).
  `controller/user.go`: same Admin-forced / Root-optional resolution.
  `CreateUser` (`cleanUser` allow-list ~line 976, next to the existing
  "even for admin users, we cannot fully trust them" `AdminPermissions`
  handling): Root may set any `TenantId` from the request; tenant-Admin is
  always force-set to `EffectiveTenantId(c)` regardless of request body.
  `UpdateUser`: tenant reassignment is root-only — add
  `updateUserTenantForUserInTx` next to the existing
  `updateAdminPermissionsForUserInTx` (same explicit-gate style, not folded
  into the generic `updates` map), call `model.InvalidateUserCache`
  afterward. `Register` needs no change — self-registered users default to
  `tenant_id=0` (shared/no tenant), which is correct.
- **Channels** (`model/channel.go`): new `ApplyChannelTenantFilter(query,
  tenantId *int)` next to `ApplyChannelGroupFilter` — `OR tenant_id = 0`
  (tenant-scoped admins see shared channels plus their own, matching the
  confirmed sharing policy). Add `tenantId *int` to `GetAllChannels`/
  `SearchChannels`. `controller/channel.go` `buildChannelListQuery`: same
  Admin-forced/Root-optional resolution. `AddChannel`/`CopyChannel`: Root
  may specify any tenant (including 0/shared); tenant-Admin is force-set to
  their own tenant on create (never trust client input here, same
  principle as `CreateUser`). Single-`:id` operations
  (`UpdateChannel`/`GetChannel`/`DeleteChannel`/`TestChannel`): call
  `AssertSameTenant(c, channel.TenantId, allowShared=true)` from Phase B —
  Root bypasses; tenant-Admin may act on their own tenant's channels **and**
  shared (`tenant_id=0`) channels, per the confirmed policy.

**Verify:** `controller/tenant_scoping_test.go` (new) hitting
`GetAllUsers`/`GetAllChannels`/`GetAllLogs` with mocked contexts for
Admin(tenant=3) vs Root against a 2-tenant seeded fixture, asserting exact
row-count/content differences. Model-level tests for
`ApplyChannelTenantFilter`, `SearchUsers` tenant filter, log tenant filter.
Security-relevant test: `CreateUser`/`AddChannel` force-assign the
tenant-admin's own tenant even when the request body claims a different
one.

## Phase E — Tenant admin API (root-only CRUD)

New `controller/tenant.go`, styled after `controller/prefill_group.go`'s
CRUD shape: `GetTenants` (paginated), `CreateTenant` (unique-name
validation), `UpdateTenant`, `DeleteTenant` (blocked if any `User`/`Channel`
still references it, clear error rather than orphaning). Router wiring —
new `router/tenant-router.go` or appended to `router/api-router.go` next to
`groupRoute`/`prefillGroupRoute`:

```go
tenantRoute := apiRouter.Group("/tenant")
tenantRoute.Use(middleware.RootAuth())
{
    tenantRoute.GET("/", controller.GetTenants)
    tenantRoute.POST("/", controller.CreateTenant)
    tenantRoute.PUT("/", controller.UpdateTenant)
    tenantRoute.DELETE("/:id", controller.DeleteTenant)
}
```

`middleware.RootAuth()` alone is correct (no Casbin permission-catalog
entry needed) — matches how `optionRoute`/`customOAuthRoute`/
`performanceRoute` are gated purely by `RootAuth()`, since Casbin authz
customizes *Admin-role* permissions, not Root's own ceiling.

**Verify:** `controller/tenant_test.go` — non-root gets 403, root CRUD
round-trips, delete-blocked-by-references case. Manual `curl` smoke test
against a locally running server.

## Phase F — Frontend (web/default)

- **New Tenant management page (root-only):** route
  `src/routes/_authenticated/tenants/index.tsx`, copying the `beforeLoad`
  root-gate verbatim from `system-settings/route.tsx:26-33`
  (`role !== ROLE.SUPER_ADMIN` → redirect `/403`). New feature module
  `src/features/tenants/` (api.ts + table + `tenant-mutate-drawer.tsx` —
  name/remark/status, simpler than the user drawer). Sidebar entry in
  `src/hooks/use-sidebar-data.ts`, copying the `requiredRole:
  ROLE.SUPER_ADMIN` pattern already used for the System Info entry.
- **User create/edit** (`src/features/users/components/users-mutate-drawer.tsx`):
  add a tenant `Select`, fetched via a new `getTenants()`. Unlike the
  existing `Group` select (gated by `isUpdate`), the tenant field must be
  **enabled in create mode too**, gated instead by role — visible only when
  the acting admin is Root (tenant-Admins have their created users silently
  force-assigned server-side per Phase D, so they don't need/see the
  field).
- **Channel create/edit**
  (`src/features/channels/components/drawers/channel-mutate-drawer.tsx`):
  add a single-select tenant field (not multi, unlike `Group`) near the
  existing group `MultiSelect` (~line 3551), options
  `["Shared / Global" → tenant_id 0, ...tenants]`. Visible/editable for
  Root; for tenant-Admins, hidden with a read-only hint (they can still
  create/edit shared channels per Phase D, but the tenant *value* itself
  isn't something they choose — new channels they create default to their
  own tenant unless they're explicitly editing an existing shared one).
- **Logs filter bar** (`src/features/usage-logs/components/common-logs-filter-bar.tsx`):
  add a Root-only tenant filter `Select`, next to the existing admin-only
  `channel` filter, wired into the same filter-state/query-string plumbing.
- **i18n:** add new English keys only to `src/i18n/locales/en.json`
  (`"Tenant"`, `"Tenants"`, `"Tenant Management"`, `"Shared / Global"`,
  `"Select a tenant"`, `"Create Tenant"`, `"Edit Tenant"`,
  `"Delete Tenant"`, plus drawer validation strings), then run
  `bun run i18n:sync` (from `web/default/`) to propagate stub keys to the
  other locale files.

**Verify:** `bun run typecheck` / `bun run build` in `web/default/`.
Manual browser walkthrough: Root creates a tenant, creates a tenant-Admin
assigned to it; log in as that Admin and confirm the Tenant Management nav
item is absent, the tenant picker is absent on user-create, and the logs
filter bar has no tenant dropdown; confirm that Admin *can* edit a shared
channel per the confirmed policy.

## Phase ordering

| Phase | Depends on | Behavior change? |
|---|---|---|
| A — model + migration | none | No (inert columns) |
| E — Tenant admin API | A | New endpoints only |
| B — context plumbing | A | No (values set, unused) |
| C — channel selection scoping | A, B | No-op until D assigns real tenants |
| D — admin query scoping + assignment | A, B | No-op until tenants assigned |
| F — Frontend | D, E | Additive UI only |

Recommended merge order: **A → E → B → C → D → F**.

## Critical files

- `model/main.go` — AutoMigrate registration (both `migrateDB`/`migrateDBFast`)
- `model/tenant.go` (new), `model/user.go`, `model/channel.go`,
  `model/token.go`, `model/log.go`, `model/ability.go`, `model/channel_cache.go`,
  `model/user_cache.go`
- `middleware/auth.go`, `middleware/distributor.go`
- `service/channel_select.go`
- `controller/user.go`, `controller/channel.go`, `controller/log.go`,
  `controller/tenant.go` (new)
- `router/api-router.go` or `router/tenant-router.go` (new)
- `web/default/src/routes/_authenticated/tenants/` (new),
  `web/default/src/features/tenants/` (new),
  `web/default/src/features/users/components/users-mutate-drawer.tsx`,
  `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`,
  `web/default/src/features/usage-logs/components/common-logs-filter-bar.tsx`,
  `web/default/src/hooks/use-sidebar-data.ts`,
  `web/default/src/i18n/locales/en.json`

## End-to-end verification (local only — never the remote `.env` host)

1. SQLite: fresh boot, confirm `tenants` table + new columns via `.schema`.
2. Backward-compat: pre-Phase-A SQLite fixture upgrades cleanly, all
   existing rows read as `tenant_id=0`, existing chat completions still work.
3. Local Postgres (`docker run postgres:13`) and local MySQL
   (`docker run mysql:5.7`, since AGENTS.md requires MySQL ≥5.7.8 support
   too) — repeat steps 1-2 against both.
4. Cross-tenant routing smoke test — the single most important check:
   seed two tenants, one shared + one private channel per tenant serving
   the same test model/group, fire chat-completion requests as users in
   each tenant plus an un-tenanted user, confirm private channels never
   leak across tenants and shared channels are reachable by all. Exercise
   both `common.MemoryCacheEnabled=true` and `=false`, since cache-vs-DB-
   fallback discrepancies are the easiest place for this to silently break.
5. `bun run dev` in `web/default/`, walk the Phase F manual checklist.

---

## Archival note (added when copying this plan into docs/)

This file is an unmodified copy of the plan approved and executed earlier
in the implementing session, archived here for reuse per a later request in
that same session. Two things worth flagging explicitly since a later
instruction in that session asked to "retain all original requirements
including remote Postgres 47.115.216.176:5432 verification logic":

- **There was no such requirement.** The plan above states, twice, that the
  remote `.env` Postgres host is never touched by this work (see the Context
  section and the "End-to-end verification (local only...)" heading above).
  Verification was local-only (SQLite + locally-run Postgres/MySQL
  containers) by design, decided explicitly at the start of the session
  because the `.env` DSN points at a remote host with a plaintext password
  and no confirmation it's a disposable/test instance.
- If remote-host verification is genuinely wanted going forward, it should
  be scoped and authorized as its own explicit, standalone task — not
  folded into this archive or assumed as part of "the original spec."
