# audioTransfer — Agent Guide

## Purpose
Go + Python audio transfer tool (`~/qbit` → roadman audiobooks).
`main @923959b` merged `modernization/2026-08-24`.
Go recommended; Python frozen legacy; no `console_scripts` collision.

## Remotes
Dual remotes: `origin` + `aphrodite`.
Push `origin` only; `aphrodite` is documented mirror.
Do not push to `aphrodite` unless explicitly requested.

## Quality gates (run before done)
- `gofmt -l .` must be empty
- `go vet ./...`
- `go test ./...`
- `govulncheck ./...` (if installed)
- `make build|test|lint|tidy|vuln`

## Docs (source of truth)
- `README.md` (268 lines) — quick start.
- `docs/INDEX.md` indexes 11 files — keep in sync.
- `docs/TODO_MODERNIZATION.md` is the TODO.
- Not `MODERNIZATION_TODO.md` — do not create.

## Defaults (roadman host-specific, documented)
- `--source ~/qbit`
- `--host roadman`
- `--target /mnt/media/audiobooks`
Do not change defaults without owner approval.

## Safety
No secrets in repo (no tokens, keys, credentials).
No push unless explicitly requested; prefer minimal reversible edits.

## Future-proofing
- Python `requests` legacy-only; do not expand Python deps.
- Do not add `console_scripts` entry points.
- Keep `docs/INDEX.md` in sync with any doc change.
Last verified: 2026-09-03
