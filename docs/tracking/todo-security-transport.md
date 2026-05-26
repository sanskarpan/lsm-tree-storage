# TODO Security and Transport Track

This document covers the issues that affected auth, network exposure, and runtime hardening.

## Scope

- [#38](https://github.com/sanskarpan/lsm-tree-storage/issues/38) `todo.md item 16 - Security defaults were wide open`
- [#39](https://github.com/sanskarpan/lsm-tree-storage/issues/39) `todo.md item 17 - Frontend codebase had two overlapping implementations`
- [#40](https://github.com/sanskarpan/lsm-tree-storage/issues/40) `todo.md item 18 - Point and range reads were still expensive at scale`
- [#41](https://github.com/sanskarpan/lsm-tree-storage/issues/41) `todo.md item 19 - Backend was exposed with permissive transport defaults`
- [#42](https://github.com/sanskarpan/lsm-tree-storage/issues/42) `todo.md item 20 - Backend APIs accepted unbounded or weakly-validated JSON`
- [#43](https://github.com/sanskarpan/lsm-tree-storage/issues/43) `todo.md item 21 - API and observability endpoints had no authentication boundary`
- [#44](https://github.com/sanskarpan/lsm-tree-storage/issues/44) `todo.md item 22 - WebSocket clients could consume resources indefinitely`
- [#45](https://github.com/sanskarpan/lsm-tree-storage/issues/45) `todo.md item 23 - The Bun BFF still had proxy-level hardening gaps`
- [#46](https://github.com/sanskarpan/lsm-tree-storage/issues/46) `todo.md item 24 - Standard-library vulnerabilities depended on the caller's Go runtime`
- [#47](https://github.com/sanskarpan/lsm-tree-storage/issues/47) `todo.md item 25 - Remote BFF exposure still depended on external auth and header policy`

## Delivery Goals

- Make network exposure explicit instead of permissive by default.
- Enforce auth and body limits at the service boundary.
- Bound WebSocket resource use and proxy behavior.
- Remove duplicated frontend implementation paths that increase drift and risk.

## Acceptance Criteria

- Unauthenticated access is denied where required.
- Oversized or malformed input is rejected early.
- WebSocket and proxy resources are bounded.
- The frontend has one authoritative implementation path.

## Validation Expectations

- Security regression checks for auth and body limits.
- WebSocket slow-client and timeout tests.
- Frontend/bff path verification for the single implementation.

## Notes

- These issues are mainly about shrinking the attack surface and making failure modes explicit.
- Hardening should be measured in behavior, not just configuration defaults.
