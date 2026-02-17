# Specs Workflow

`docs/specs/` holds feature-level specs that drive implementation and tests.

## V1 Policy

`localclaw` is still in v1 build-out. Specs should prefer clean implementations and should not require:
- rollback plans
- fallback execution paths
- legacy compatibility shims

## When to Add a Spec

Add or update a spec for:
- non-trivial behavior changes
- new user-visible CLI/TUI workflows
- runtime boundary changes
- config/policy contract changes

## Spec-Driven + Test-Driven Loop

1. Write or update spec first.
2. Define acceptance criteria and concrete test plan.
3. Implement via Red/Green cycles with focused tests.
4. Confirm full-suite pass (`go test ./...`).
5. Mark spec status and link to implementation PR/commit.

## Suggested File Naming

- `YYYY-MM-DD-<short-feature-name>.md`
- or `<feature-name>.md` when keeping existing repo style.

## Required Sections

- Status
- V1 build policy note
- Problem / Motivation
- Scope
- Behavior contract
- Test plan
- Acceptance criteria
- Out-of-scope notes

See `template.md` for a starting point.
