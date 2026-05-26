# TODO State and Bootstrap Track

This document covers the issues that affected startup wiring, frontend entrypoints, and observable write behavior.

## Scope

- [#28](https://github.com/sanskarpan/lsm-tree-storage/issues/28) `todo.md item 6 - Memtable retained caller-owned byte slices`
- [#29](https://github.com/sanskarpan/lsm-tree-storage/issues/29) `todo.md item 7 - Backend startup ignored config.yaml`
- [#30](https://github.com/sanskarpan/lsm-tree-storage/issues/30) `todo.md item 8 - Backend and BFF ports were hardcoded`
- [#31](https://github.com/sanskarpan/lsm-tree-storage/issues/31) `todo.md item 9 - Frontend entrypoint was a placeholder`
- [#32](https://github.com/sanskarpan/lsm-tree-storage/issues/32) `todo.md item 10 - Frontend dashboard hardcoded localhost:3001`
- [#33](https://github.com/sanskarpan/lsm-tree-storage/issues/33) `todo.md item 11 - WAL append events were missing from normal writes`

## Delivery Goals

- Remove bootstrap ambiguity across backend, BFF, and frontend startup.
- Ensure caller-owned data is copied before storage-layer retention.
- Load configuration from the repo’s declared runtime files instead of hardcoded assumptions.
- Keep write events visible to the dashboard and diagnostics path.

## Acceptance Criteria

- Clean startup works from a documented config and port set.
- Frontend boots through the real runtime entrypoint.
- Write event streams include normal writes, not only special paths.
- Buffer ownership is explicit and regression-tested.

## Validation Expectations

- Startup and config-loading tests.
- Frontend build and entrypoint verification.
- Event-stream checks for write visibility.

## Notes

- These issues were not just convenience bugs; they were the cause of startup drift and missing observability.
- Any change here should be tested against a real boot sequence, not only unit-level mocks.
