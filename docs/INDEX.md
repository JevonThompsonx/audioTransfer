# audioTransfer docs index

11 files. Start with README.md at repo root, then TODO_MODERNIZATION.md.

## Modernization / planning

- [TODO_MODERNIZATION.md](TODO_MODERNIZATION.md) — Modernization Audit TODO (current work list)
- [TRANSFER_FIXES_PLAN.md](TRANSFER_FIXES_PLAN.md) — Virus Scan, Resumed Counting, Source Deletion fixes plan
- [TRANSFER_NOTES.md](TRANSFER_NOTES.md) — Transfer Notes & Findings

## Library audits (one-off runs, kept for reference)

- [FULL_METADATA_AUDIT_PLAN.md](FULL_METADATA_AUDIT_PLAN.md) — Full Library Metadata Audit & Repair Plan
- [FULL_METADATA_AUDIT_RESULTS.md](FULL_METADATA_AUDIT_RESULTS.md) — Full Library Metadata Audit — Results
- [FULL_METADATA_AUDIT_ROUND2.md](FULL_METADATA_AUDIT_ROUND2.md) — Full Library Metadata Audit — Round 2 (2026-08-15)
- [FULL_METADATA_AUDIT_RUNLOG.md](FULL_METADATA_AUDIT_RUNLOG.md) — Full Metadata Audit — Run Log
- [SERIES_AUDIT_PLAN.md](SERIES_AUDIT_PLAN.md) — ABS Library Series Audit & Repair Plan
- [SERIES_AUDIT_RESULTS.md](SERIES_AUDIT_RESULTS.md) — Series Assignment Audit — Results
- [MISTBORN_ORG_PLAN.md](MISTBORN_ORG_PLAN.md) — Mistborn Series Reorganization Plan

## Operations

- [ROADMAN_QBIT_AUTOMATION.md](ROADMAN_QBIT_AUTOMATION.md) — qBittorrent → Audiobookshelf automation (roadman)

## Repo notes (not in docs/)

- Root `README.md`: Go (recommended, actively maintained) vs Python `audiobook_transfer/` (legacy, frozen).
- `--delete-source` is OFF by default and requires `--verify` (verify-gated, never deletes on scp success alone).
- Remotes: `origin` (GitHub, canonical push target) + `aphrodite` (fetch remote for the Aphrodite checkout; do not push here unless owner names it).
