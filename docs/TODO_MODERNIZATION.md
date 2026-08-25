# audioTransfer — Modernization Audit TODO

**Scope**: `/home/hermes/Projects/audioTransfer` (Go+Python audiobook transfer/organizer).
**Canonical impl**: Go (`cmd/`, `pkg/`). **Python** (`audiobook_transfer/`) is explicitly LEGACY/frozen per README §`README.md:209`.
**Date**: 2026-08-24 · **Auditor**: subagent audit pass.
**Wave 1 status (2026-08-24)**: branch `modernization/2026-08-24` @ `e539b97fc349b93867fc2be522ee62305ac27e16` — 2 source commits landed (gofmt + CI). See "🏁 Wave 1 Status" below.

## Legend
- **P0** = security/correctness blocker, do first. **P1** = high-value efficiency/DX. **P2** = nice-to-have.
- **Effort**: S (<½d) · M (½–2d) · L (>2d). **Impact**: user/dt-facing severity.

---

## 🔴 SECURITY

- [ ] **P0 · S · High** — Python `transfer.py` sets world-writable `chmod 777` / `chmod -R 777` (lines 226–230, 351–359) for both local and remote targets. Go version correctly uses `755`/`644`. Frozen code, but if ever reused it's a privilege/integrity risk on a shared media box. Fix or delete the legacy impl (see DX-2).
- [ ] **P1 · S · Med** — Default SSH user is `root` (`pkg/transfer/transfer.go:24` `DefaultUser = "root"`, `audiobook_transfer/transfer.py:16` `DEFAULT_USER = "root"`). Connecting as root to copy media is unnecessary blast radius. Default to a non-privileged user; require explicit opt-in for root.
- [ ] **P1 · M · Med** — `scripts/qbit-postprocess.sh` reads plaintext qBittorrent creds from `/root/.qbit-webui-password` (line 53–54). No perms check / no fail-closed if world-readable. Add `chmod 600` enforcement + a presence/perms guard, or move to a secrets manager.
- [ ] **P2 · S · Low** — `StrictHostKeyChecking=accept-new` is used everywhere (transfer.go, virusscan/remote.go). First-run TOFU is acceptable, but SSH host keys are never pinned/persisted for verification across runs — consider a known_hosts pin for the production host.
- [ ] **P2 · S · Low** — Path-traversal guard `validateSubpath` (transfer.go:473) only checks `filepath.Clean` output for literal `".."`; an absolute path is silently stripped of its leading `/`. Adequate but undocumented; add a comment + a unit test asserting `/etc/passwd`-style inputs are rejected.

## ⚡ EFFICIENCY / SPEED / PERFORMANCE

- [ ] **P1 · M · High** — `LocalScanner.Workers = 4` (virusscan/local.go:22) is **declared but never used**. Scanning is fully sequential per 200/500-file batch. ClamAV scans of large libraries (the docs mention multi-hour runs) would benefit from a real worker pool. Implement bounded parallelism or delete the dead field.
- [ ] **P1 · M · Med** — `scanner.ScanDirectory` walks the tree **multiple times per entry**: `hasAudioInDir` + `WalkDir` + `hasAnySubBookDir` + nested `hasAnyAudio` (scanner.go:103–215). For deep libraries this is O(depth × files). Collapse into a single `WalkDir` pass that records audio/cover presence and child-dir audio flags once.
- [ ] **P1 · S · Med** — `RemoteScanner.ScanFiles` (virusscan/remote.go:61) shells out one `clamscan` per distinct parent **directory**, each over SSH with a 30-min timeout, sequentially. Batching all paths per remote dir into one `clamscan -r` invocation (as `ScanDir` already does) would cut SSH round-trips dramatically.
- [ ] **P2 · M · Med** — No streaming/copy progress + `copyFile` (transfer.go:485) does `defer out.Close()` **without checking the close error** — a truncated final write on a full disk is silently swallowed. Check `out.Close()` and surface errors.
- [ ] **P2 · S · Low** — `transferFile` SCP timeout is a flat `30*time.Minute` (transfer.go:296) regardless of file size; large books can time out or small files wait unnecessarily. Make it size-aware or use a streaming progress + kill-on-stall.
- [ ] **P2 · S · Low** — `RemoteScanner.ScanFiles` drops any result file not under a scanned dir (`fileSet[r.File]` filter, remote.go:93) and the `len(paths)==0` branch is dead (ScanFiles returns early when empty). Files outside the computed dir set vanish from the report — silent under-reporting of scan coverage.

## 🛠 QUALITY OF LIFE / DX

- [x] **P0 · S · High — DONE in Wave 1** — **No CI at all** (no `.github/workflows`, Makefile, or pre-commit). `gofmt -l` flagged 4 files (`metadata.go`, `metadata_test.go`, `parser_test.go`, `transfer_test.go`). **Resolved by Wave 1:** `f5982ab` (`gofmt -w .`) + `e539b97` (added `.github/workflows/ci.yml` running gofmt-vet-test-build). Verified `gofmt -l .` clean post-Wave-1.
- [ ] **P0 · S · High** — **README architecture section is stale**: `README.md:205` still says `metadata/metadata.go` is "OpenLibrary Search API client" only, and omits virus-scan, the Audible→audnex→iTunes→OpenLibrary provider chain, checkpoint resume, `--delete-source`, and parallel transfers. Update the diagram + pipeline description to match current code.
- [ ] **P1 · M · Med** — **Two divergent implementations** of the same tool (Go canonical + Python legacy). They have already drifted: Python lacks virus-scan, checkpoint/resume, delete-source, parallelism, and the rich metadata chain (only OpenLibrary), and still has the `chmod 777` bug. Decide: delete the Python package (recommended — it's marked frozen) or lock it behind a warning + CI that fails if it diverges.
- [ ] **P1 · M · Med** — **Dependency hygiene**: `go.mod` declares only `go 1.21` with **zero external requires** (pure stdlib — good for supply chain), but `go.sum` is absent and `go mod tidy` was never run; `go.mod` is also behind the installed toolchain (1.23.4). Bump `go 1.23`, run `go mod tidy`, and consider `go mod verify` in CI. Python `pyproject.toml` pins `requires-python = ">=3.8"` (3.8 is EOL) and `paramiko` is unpinned — bump to `>=3.10` and pin optional deps.
- [ ] **P1 · S · Med** — **Hardcoded environment constants** baked into source: `roadman`, `/mnt/media/audiobooks`, `/mnt/media/qbit`, qBittorrent `127.0.0.1:8081`, `CREDS=/root/.qbit-webui-password`. These belong in env vars / a config file with sane defaults, so the tool isn't roadman-only. (The `qbit-postprocess.sh` script is even more environment-coupled.)
- [ ] **P2 · M · Low** — **Test coverage gaps**: Go has good unit tests (parser, scanner, transfer, virusscan, organizer, metadata) but **no tests** for `models`, `utils`, `virusscan/factory.go`, `virusscan/parsers.go`, and **no integration/e2e** test. Python has **zero tests** for any module. Add at least `models`/`utils` unit tests and one end-to-end dry-run test.
- [ ] **P2 · S · Low** — **Docs sprawl**: `docs/` holds 9 audit/plan/runlog markdown files (`FULL_METADATA_AUDIT_*`, `SERIES_AUDIT_*`, `TRANSFER_*`, etc.). Several are historical and now misleading (e.g. note in delete_source.go:228 "inert today (see TRANSFER_FIXES_PLAN §0)"). Consolidate into one living `STATUS.md` and archive the rest, or they'll keep lying to future readers.
- [ ] **P2 · S · Low** — **No `Makefile`/`justfile`** for common tasks (`build`, `test`, `lint`, `install`). A one-screen `make` would lower contributor friction given the dual-language layout.

## Summary counts
- **P0**: 3 (legacy chmod 777, no CI, stale README) · **P1**: 7 · **P2**: 8 · **Total**: 18.
- **Biggest leverage**: add CI + gofmt/lint gate (catches the 4 unformatted files immediately), reconcile/delete the legacy Python impl, and fix the unused `Workers` + redundant scanner walks for real speed wins.

---

## 🏁 Wave 1 Status — 2026-08-24

**Branch:** `modernization/2026-08-24` · **HEAD SHA:** `e539b97fc349b93867fc2be522ee62305ac27e16` (short `e539b97`)
**Wave 1 commits (source changes, already landed):**
- `f5982ab` — style: gofmt 4 files (resolves the 4 `gofmt -l` failures from audit P0/DX-1).
- `e539b97` — ci: add gofmt/vet/test/build gates (`.github/workflows/ci.yml`).

**Completed in Wave 1:**
- [x] **P0 · S · High — No CI (DX-1).** Added `.github/workflows/ci.yml` running `gofmt -l` (empty gate), `go vet ./...`, `go test ./...`, `go build ./...`. The 4 previously-unformatted files are now formatted (verified `gofmt -l .` clean after `f5982ab`).
- [x] **P0 · S · High — `gofmt -l` flagged 4 files.** Resolved by `f5982ab` (`gofmt -w .`).

**NOT done in Wave 1 (still open — see Wave 2):**
- README architecture bullet still stale (P0 DX-2): *partially mitigated* — working tree had uncommitted README edits at baseline; verify final committed state.
- `go.mod` still `go 1.21` (not bumped to 1.23); `go mod tidy` not run (P1 DX).
- Python legacy impl, hardcoded env constants, scanner perf, root-default, qbit creds, etc. — all still open.

---

## 🔧 Corrections from Baseline + Second Audit (2026-08-24)

These refine the original audit; apply before/while doing Wave 2:

- **`copyFile` close-error claim is FALSE (was P2 EFF).** `copyFile` returns `out.Close()` (transfer.go:501); only a redundant `defer out.Close()` at :496 remains. Action: drop the stray `defer`, do **not** add a missing check. *(Baseline + Second Audit both confirmed; audit item was OVERSTATED.)*
- **Remote per-dir SSH clamscan batching targets DEAD CODE (was P1 EFF).** `RemoteScanner` is never invoked — `organizer.RunTransfer` hardcodes `NewScanner("local", …)` (organizer.go:927). Reframe as **"delete `RemoteScanner` + dead `escapePath`"**, not "optimize it."
- **`root` default (was P1 SEC) is actually HARDWIRED.** No `--user` flag exists; `transfer.NewClient` always injects `DefaultUser="root"`. Raise to: *add `--user` flag, default non-root, stop hardwiring root.* (Second Audit S2, High.)
- **NEW High — `--delete-source` ON by default, `--verify` OFF by default** (main.go:39 vs :37). Contradicts `TRANSFER_FIXES_PLAN.md §3.1` ("opt-in only"). Ad-hoc runs delete local source on unverified scp success → data-loss posture. (Second Audit S1/R1, High — must fix before any merge.)
- **NEW Med — Python console-script `audiotransfer` collides with the Go binary** (`pyproject.toml` script name == Go `cmd/audiotransfer`). `pip install` shadows the safe Go impl with the chmod-777/root Python impl. Rename Python entrypoint or drop the script. (Second Audit S6/D4.)
- **NEW Low — `shutil.rmtree(…, ignore_errors=True)`** silently swallows deletion errors (organizer.py:68, utils.py:91). (Second Audit S7.)
- **Scanner multi-walk is EXPONENTIAL, not constant** (was P1 EFF, understated). Nested `Author/Series/Title` trees re-walk subtrees repeatedly. Sharpen fix to "single stateful WalkDir." (Second Audit R3.)
- **delete-source safety IS unit-tested** (`delete_source_test.go`). Audit's "no tests for models/utils/factory/parsers" is accurate, but the delete-safety path is covered — audit was partially inaccurate there. (Second Audit D10.)
- **Duplicated `escapeSSH`/`escapePath`** are byte-identical (transfer.go:468 vs remote.go:226). Delete with `RemoteScanner`. (Second Audit D9.)

---

## 🌊 Wave 2 Queue (priority order)

1. **HIGH — Flip `--delete-source` default to OFF**; require `--verify` when deleting; make the qBittorrent hook explicit. (S1/R1)
2. **HIGH — Add `--user` flag; default non-root; stop hardwiring `DefaultUser`** into `NewClient`. (S2)
3. **MED — Rename/remove Python `audiotransfer` console-script** so it can't shadow the Go binary. (S6/D4)
4. **MED — `go mod edit -go=1.23` + `go mod tidy`**; keep `go mod verify` in CI. (P1 DX)
5. **MED — Remove dead `Workers` field** or implement the pool in the local scanner. (R2/P1)
6. **MED — Collapse scanner multi-walk** to a single stateful WalkDir. (R3/P2 understated)
7. **MED — Delete `RemoteScanner` + duplicated `escape*` helpers** (dead code). (R4/D9)
8. **MED — Harden `qbit-postprocess.sh`** creds perms (`chmod 600` enforcement + guard) + `HOME` handling. (S4)
9. **MED — Decide Python fate** (delete recommended) + fix README architecture bullet. (D2/D3)
10. **LOW — `shutil.rmtree ignore_errors=True`** fix (Python temp cleanup). (S7)
11. **LOW — Extract shared config** for hardcoded env constants (`roadman`, `/mnt/media/*`, creds path). (D6)
12. **LOW — Add `Makefile`/`justfile`**, consolidate `docs/` sprawl into a `STATUS.md`. (D7/D8)
13. **LOW — Add `models`/`utils`/`factory`/`parsers` unit tests** + one e2e dry-run. (D11)
