# CHECKLIST Frontend and Integration Track

This document tracks the dashboard, UI panels, and end-to-end polish work.

## Scope

- [#17](https://github.com/sanskarpan/lsm-tree-storage/issues/17) `CHECKLIST Phase 15 - Frontend Panel 1: Write Path Visualizer (12 tasks)`
- [#18](https://github.com/sanskarpan/lsm-tree-storage/issues/18) `CHECKLIST Phase 16 - Frontend Panel 2: LSM Level Tree (14 tasks)`
- [#19](https://github.com/sanskarpan/lsm-tree-storage/issues/19) `CHECKLIST Phase 17 - Frontend Panel 3: Bloom Filter (10 tasks)`
- [#20](https://github.com/sanskarpan/lsm-tree-storage/issues/20) `CHECKLIST Phase 18 - Frontend Panels 4–7 (22 tasks)`
- [#21](https://github.com/sanskarpan/lsm-tree-storage/issues/21) `CHECKLIST Phase 19 - Integration Tests + Polish (14 tasks)`

## Delivery Goals

- Turn the UI into a cohesive operational dashboard rather than a collection of panels.
- Keep the frontend aligned with real backend and BFF behavior.
- Make integration tests cover the full write-to-observe loop.
- Preserve visual polish without hiding correctness issues.

## Acceptance Criteria

- Each panel has a clear responsibility and a stable data contract.
- Frontend state is bounded and testable.
- Dashboard interactions reflect real live server state.
- End-to-end tests cover startup, writes, reads, and websocket updates.

## Validation Expectations

- TypeScript and build checks for the client.
- Live browser checks against backend and BFF.
- Integration tests that exercise all panels together.

## Notes

- This track is intentionally the last checklist phase because it depends on the preceding storage and API layers being trustworthy.
- Integration bugs here are usually contract drift, not isolated UI issues.
