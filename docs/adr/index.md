# Architecture Decision Records

Architecture decisions are documented using the [Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions). New ADRs should be created as `docs/adr/NNNN-title.md`.

| Number | Title | Status | Date |
|--------|-------|--------|------|
| [ADR-0001](0001-lsm-over-btree.md) | LSM-Tree over B-Tree | Accepted | 2025-01 |
| [ADR-0002](0002-raft-replication.md) | Replicate Logical Operations, Not SSTables | Accepted | 2025-01 |
| [ADR-0003](0003-write-before-disk.md) | Write-Before-Disk Atomicity in MANIFEST | Accepted | 2025-03 |
| [ADR-0004](0004-seqno-persistence.md) | Persist MaxSeqNo to MANIFEST on Every Flush | Accepted | 2025-03 |
