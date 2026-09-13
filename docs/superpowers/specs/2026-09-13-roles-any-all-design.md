# Role Requirement Semantics: Any-Of And All-Of

## Goal

Make role requirements explicit and intuitive. Today `auth.roles: [a, b]` requires **all** listed roles (AND), which is surprising and undocumented in behavior. Change the default to **any-of** and add an explicit **all-of** field, while keeping the failure mode for a missing or malformed `roles` claim at 401.

## Semantics

- `auth.roles: [a, b]` — at least one of the listed roles must be present (OR). Default.
- `auth.roles_all: [a, b]` — every listed role must be present (AND).
- If both are set, the effective requirement is `(any of roles) AND (all of roles_all)`.
- If neither is set, no role check is performed.
- If a route requires roles (either field non-empty) and the token has no `roles` claim, or the claim is neither a string nor an array of strings, the request fails with 401 and a clear error.
- The `roles` claim may be a single string or an array; both are accepted.

## Configuration And Discovery

- Add `roles_all` to `AuthRule` (`internal/config/config.go`).
- Extend the discovery label contract with `gateway.router.<id>.auth.roles_all` (comma-separated), mirroring `auth.roles`.
- Keep `auth.roles` label behavior; add `auth.roles_all`.
- Update `README.md` and the documentation reference to describe both fields.

## Documentation

- Update the canonical configuration reference (`/docs/api-gateway/configuration`) and scenarios to explain any-of vs all-of, with examples.
- Update the mirrored copy in the gateway repository via the sync script.
- Add a scenario showing "admin or support" (any) and "admin and mfa-verified" (all).

## Success Criteria

- Existing configs using `roles` now mean any-of; the change is called out as breaking.
- Tests cover: any-of success/failure, all-of success/failure, combined any+all, missing claim, malformed claim, string claim, and discovery label parsing.
- No behavior change when no roles are configured.
- Docs and README describe the exact semantics.
