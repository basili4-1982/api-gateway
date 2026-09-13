# Roles Any-Of / All-Of Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change `auth.roles` to any-of, add `auth.roles_all` for all-of, keep missing/malformed `roles` claim as 401, and document both.

**Architecture:** Add `RolesAll` to `AuthRule` and parse it from discovery labels. Rewrite `checkRoles` to evaluate any-of and all-of over a parsed role set, failing closed on a missing or malformed claim. Update docs and README.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `gopkg.in/square/go-jose` JWT claims, Docker/Podman labels.

## Global Constraints

- Use `-buildvcs=false` for Go commands in worktrees.
- No behavior change when no roles are configured.
- Missing or malformed `roles` claim on a role-protected route must return 401.
- Update `README.md` and the documentation reference; the site docs are canonical.
- Every change ships with tests; run `go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run`.

---

### Task 1: Config field and role evaluation

**Files:**
- Modify: `internal/config/config.go` (`AuthRule`)
- Modify: `internal/proxy/multi_proxy.go` (`checkRoles`, caller)
- Test: `internal/config/config_test.go`, `internal/proxy/proxy_test.go`

**Interfaces:**
- Produces: `AuthRule.RolesAll []string`.
- Produces: `checkRoles` semantics: `(any of Roles) AND (all of RolesAll)`; missing/malformed claim → error.

- [ ] Write failing tests: any-of success/failure; all-of success/failure; combined; missing claim; malformed claim; single-string claim.
- [ ] Add `RolesAll []string \`yaml:"roles_all"\`` to `AuthRule`.
- [ ] Rewrite `checkRoles` accordingly; keep the caller condition for either field.
- [ ] Run `go build`, `go vet`, `go test -race ./internal/config/ ./internal/proxy/`.
- [ ] Commit `feat(auth): any-of roles by default with explicit roles_all`.

### Task 2: Discovery labels

**Files:**
- Modify: `internal/discovery/labels.go`
- Test: `internal/discovery/labels_test.go`

**Interfaces:**
- Produces: `gateway.router.<id>.auth.roles_all` parsed into `AuthRule.RolesAll`.

- [ ] Write a failing label test for `auth.roles_all`.
- [ ] Parse the label alongside `auth.roles`.
- [ ] Run discovery tests with `-race`.
- [ ] Commit `feat(discovery): support auth.roles_all label`.

### Task 3: Docs and README

**Files:**
- Modify: `README.md`
- Site canonical: `frontend/app/docs/api-gateway/configuration/content.md` and `scenarios/content.md` (in the web-sarnas repo)
- Mirror: `docs/api-gateway-configuration.md`, `docs/api-gateway-scenarios.md` (in the gateway repo, generated)

- [ ] Update README and the configuration reference: `roles` = any, `roles_all` = all, missing/malformed claim → 401.
- [ ] Add a scenario example for any-of and all-of.
- [ ] Regenerate the mirror with `scripts/sync-api-gateway-docs.sh`.
- [ ] Commit docs in the appropriate repositories.

### Task 4: Review and PR

- [ ] Run the full check suite and `git diff --check`.
- [ ] Invoke `requesting-code-review`; resolve findings.
- [ ] Push and open a PR against `master`; note the breaking semantic change in the description; merge after checks pass.
