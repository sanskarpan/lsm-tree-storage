# TODO Operations and CI Track

This document covers the remaining production-readiness issues around observability, backups, deployment, and CI.

## Scope

- [#48](https://github.com/sanskarpan/lsm-tree-storage/issues/48) `todo.md item 26 - Runtime observability was not production-friendly`
- [#49](https://github.com/sanskarpan/lsm-tree-storage/issues/49) `todo.md item 27 - There was no repo-native backup or restore workflow`
- [#50](https://github.com/sanskarpan/lsm-tree-storage/issues/50) `todo.md item 28 - There was no sustained HTTP load or soak harness`
- [#51](https://github.com/sanskarpan/lsm-tree-storage/issues/51) `todo.md item 29 - Deployment artifacts were incomplete`
- [#52](https://github.com/sanskarpan/lsm-tree-storage/issues/52) `todo.md item 30 - CI automation did not enforce the new quality bar`

## Delivery Goals

- Make the service observable enough to operate without guesswork.
- Provide repo-native backup and restore tooling.
- Add sustained load validation to catch latency and stability regressions.
- Keep deployment and CI artifacts aligned with the runtime contract.

## Acceptance Criteria

- Metrics, readiness, and health checks are defined and validated.
- Backup and restore procedures work from the repository alone.
- Load and soak testing can be run locally and in CI.
- Container and CI artifacts enforce the expected quality gates.

## Validation Expectations

- Metrics and readiness endpoint checks.
- Backup/restore smoke tests.
- Load-test runs against a live service.
- CI pipeline validation for build, test, race, and vulnerability checks.

## Notes

- This track is the operational wrapper around the earlier correctness and security work.
- If any of these pieces drift, the repo becomes hard to run safely in practice even if the code itself is sound.
