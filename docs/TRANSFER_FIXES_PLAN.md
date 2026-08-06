# AudioTransfer Fixes Plan — Virus Scan, Resumed Counting, Source Deletion

**Author:** audioTransfer planning
**Date:** 2026-08-06
**Scope:** Three changes to the actively-maintained Go implementation (`cmd/audiotransfer/` + `pkg/`): (1) fix the pre-transfer ClamAV scan breaking on machines with `clamdscan`; (2) stop counting checkpoint-restored books as "Transferred" so plan and summary reconcile; (3) add an opt-in `--delete-source` feature that removes a book's local source files only after a verified, successful transfer. **Planning only — this document does not modify any code.** The Python implementation (`audiobook_transfer/`) is legacy/frozen and is **not** touched.

---

## 0. Verified Environment Facts (this session)

- Go **1.21**, module `github.com/jevonx/audioTransfer` (`go.mod`), zero external deps. `go build ./...` currently passes.
- Active code lives in `cmd/audiotransfer/` and `pkg/` (`organizer`, `scanner`, `parser`, `metadata`, `transfer`, `virusscan`, `models`, `utils`). No test runner config / Makefile — standard `go test ./...`.
- No JSON/API contract consumes `TransferReport` or `TransferResult` — they are in-memory only (report is returned to `main`, which reads `report.Failed`). Adding fields is safe.
- `LocalScanner` args are built in **one place**, `pkg/virusscan/local.go:160` (`scanBatch`). `ScanDir` (`local.go:121`) already omits `--no-follow-symlinks` and is not on the organizer path (organizer uses `ScanFiles`).
- `DetectBinary` (`local.go:26-41`) prefers `clamdscan` when present → `UseDaemon=true`. Confirmed: `clamdscan` does **not** support `--no-follow-symlinks` (clamscan-only option); exit code 2 → every batch errors → 0 clean / 0 infected / N errors, and since the organizer only filters *infected* files, the scan provides **zero protection** on clamd machines.
- Checkpoint fast-path in `pkg/organizer/organizer.go:186-208` appends a `TransferResult{Status: entry.TransferStatus}` and bumps `report.Transferred`/`report.Local` without the book ever appearing in the `[3/5] Transfer plan` (`matched`) section — the plan-vs-summary mismatch.
- Defaults (main.go): `SourceDir=~/qbit`, `DestDir=~/qbit/organized` (DestDir is **inside** SourceDir). The scanner skips the `organized` dir by name (`scanner.go:57-61`). Any deletion design must therefore guard against deleting inside `DestDir`, not just "inside SourceDir".
- Adjacent pre-existing bug (flagged, NOT in scope): `extractAndScanZip` (`scanner.go:278-283`) runs `defer cleanup()` at function scope, so zip-extracted temp files are deleted **before** transfer. Zip books therefore can never currently transfer (files missing → `transfer.go:152` "File not found"). This makes any zip-source deletion inert until that bug is fixed. See §7 risk #5.

---

## 1. Issue 1 — `clamdscan` rejects `--no-follow-symlinks`

### 1.1 Root cause

`pkg/virusscan/local.go:158-174`:

```go
func (s *LocalScanner) scanBatch(files []string) ([]ScanResult, error) {
	args := []string{"--infected", "--no-summary", "--no-follow-symlinks"} // line 160 — unconditional
	args = append(args, files...)
	cmd := exec.Command(s.BinPath, args...)   // s.BinPath may be clamdscan
	...
}
```

On any host with `clamd` installed, `detectBinary()` picks `clamdscan` (`UseDaemon=true`), but `clamdscan` only supports `--follow-file-symlinks`/`--follow-dir-symlinks` (not `--no-follow-symlinks`). Result: exit code 2, `scanBatch` returns an error, and `ScanFiles` (`local.go:86-97`) marks every file in the batch as an error. The organizer (`organizer.go:816-871`) only filters **infected** files, so all books proceed "clean" — the scan is silently worthless.

### 1.2 Fix — `pkg/virusscan/local.go`

Make the flag conditional on `UseDaemon`, and extract args construction into a testable helper.

```go
// scanBatch runs clamscan/clamdscan on a batch of files.
func (s *LocalScanner) scanBatch(files []string) ([]ScanResult, error) {
	args := s.batchArgs(files)
	cmd := exec.Command(s.BinPath, args...)
	out, err := cmd.Output()

	// Exit code 1 = infected files found (not an error)
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("clamscan exit code %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
	}
	return parseClamOutput(string(out)), nil
}

// batchArgs builds the argument list for a batch scan. clamdscan does NOT
// support --no-follow-symlinks (clamscan-only option); it follows symlinks by
// default, which is acceptable — only pass the flag in clamscan mode.
func (s *LocalScanner) batchArgs(files []string) []string {
	args := []string{"--infected", "--no-summary"}
	if !s.UseDaemon {
		args = append(args, "--no-follow-symlinks")
	}
	return append(args, files...)
}
```

- **Required change:** `local.go:160` → conditional append; new `batchArgs` helper right below `scanBatch`.
- No change to `detectBinary`, `Preflight`, or `ScanDir` (ScanDir never used the flag). `--no-follow-symlinks` remains for clamscan exactly as today.
- Optional cosmetic: error string "clamscan exit code" → "clam exit code" so it reads correctly for both binaries.
- Optional hardening (not recommended, adds flag-surface for no behavior change): pass `--follow-file-symlinks` explicitly in daemon mode. Skip — default clamdscan behavior already follows symlinks.

### 1.3 Tests — `pkg/virusscan/scanner_test.go` (package `virusscan`)

Construct scanners **deterministically** (do not rely on `detectBinary` / PATH):

```go
func TestScanBatchArgs_DaemonMode(t *testing.T) {
	s := &LocalScanner{BinPath: "/usr/bin/clamdscan", UseDaemon: true}
	args := s.batchArgs([]string{"/tmp/a.mp3"})
	if args[0] != "--infected" || args[1] != "--no-summary" {
		t.Fatalf("unexpected leading args: %v", args)
	}
	for _, a := range args {
		if a == "--no-follow-symlinks" {
			t.Fatal("clamdscan mode must never receive --no-follow-symlinks")
		}
	}
	if len(args) < 3 || args[len(args)-1] != "/tmp/a.mp3" {
		t.Errorf("file args must be appended last: %v", args)
	}
}

func TestScanBatchArgs_ClamscanMode(t *testing.T) {
	s := &LocalScanner{BinPath: "/usr/bin/clamscan", UseDaemon: false}
	args := s.batchArgs([]string{"/tmp/a.mp3"})
	seen := false
	for _, a := range args {
		if a == "--no-follow-symlinks" {
			seen = true
		}
	}
	if !seen {
		t.Error("clamscan mode must include --no-follow-symlinks")
	}
}
```

- **Keep** `TestNewScanner_Local` as-is (still allows `local-clamscan` OR `local-clamdscan`).

---

## 2. Issue 2 — Checkpoint-restored books counted as "Transferred"

### 2.1 Root cause

`pkg/organizer/organizer.go:186-208`: a book already in the checkpoint with unchanged source files takes the fast-path, appends a `TransferResult{Status: entry.TransferStatus}` and does `report.Transferred++`/`report.Local++` (lines 201-205). It is **not** added to `matched`, so it never appears in the `[3/5] Transfer plan (%d books)` section (line 278 counts `len(matched)` only). Result: "Transfer plan (1 books)" vs "Transferred (remote): 2".

### 2.2 Fix

**A. `pkg/models/models.go`**

- `TransferResult.Status` comment: add `resumed` to the allowed set (line 78).
- `TransferReport`: add `Resumed int` (line 88 area) and `Deleted int` (Issue 3):

```go
type TransferReport struct {
	Total        int
	Transferred  int
	Resumed      int // books already transferred in a previous run (checkpoint fast-path)
	Skipped      int
	Failed       int
	Unmatched    int
	Local        int
	Infected     int
	Deleted      int // books whose local source was removed by --delete-source
	Results      []TransferResult
	MethodsTried []string
}
```

- `PrintSummary` (`models.go:104-109`): add one line after "Transferred (remote)":

```go
	fmt.Printf("  Transferred (remote): %d\n", r.Transferred)
	fmt.Printf("  Resumed (already transferred): %d\n", r.Resumed)
	fmt.Printf("  Copied (local)      : %d\n", r.Local)
```

(Label chosen as "already transferred" rather than "already on remote" because a checkpointed **local** entry — `TransferStatus == "local"` resumed on a local-only run — is not "on remote".)

**B. `pkg/organizer/organizer.go`**

1. Before the match loop (near line 176) declare `var resumedBooks []string`.
2. Fast-path (lines 193-205) → always report status `"resumed"` and count into `report.Resumed`, never `Transferred`/`Local`:

```go
			if currentSize == entry.SourceSize && currentModTime.Equal(entry.SourceModTime) {
				// Already transferred in a previous run and source unchanged —
				// count as "resumed", not as newly transferred.
				result := models.TransferResult{
					SourceName: book.Name,
					Identity:   entry.Identity,
					Status:     "resumed",
					FilesCount: entry.FilesCount,
					MethodUsed: entry.MethodUsed,
				}
				report.Results = append(report.Results, result)
				report.Resumed++
				resumedBooks = append(resumedBooks, book.Name)
				continue
			}
```

3. Early-return when nothing left to match (line 267): `if report.Transferred > 0 || report.Local > 0` → add `|| report.Resumed > 0` (otherwise an all-resumed run hits the "No books could be matched" branch).
4. `[3/5]` plan section (after the `matched` loop, ~line 283): print the resumed note so plan + summary reconcile:

```go
	if len(resumedBooks) > 0 {
		fmt.Printf("\n  Resumed (already transferred in a previous run): %d\n", len(resumedBooks))
		for _, name := range resumedBooks {
			fmt.Printf("    %s\n", name)
		}
	}
```

5. No other changes needed — `verifyTransfers` (`organizer.go:555`) and the pending/done loops only match `transferred`/`local`, so `resumed` results are naturally skipped. The `resumeSkip` path during transfer (lines 391-393) still legitimately marks `transferred` (the book IS on remote this run, verified by size) — leave as-is.

### 2.3 Tests

- `pkg/organizer/organizer_test.go` — integration test of the fast-path (make `CheckpointPath()` testable by overriding `$HOME`; Go's `os.UserHomeDir` reads `$HOME` per call on Unix):

```go
func TestCheckpointFastPathCountsResumed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Test Author - Test Book.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)
	fi, _ := os.Stat(bookFile)

	cp := &Checkpoint{Books: map[string]*CheckpointEntry{
		bookFile: { // checkpointKey(book) == AudioFiles[0]
			Identity:       &models.BookIdentity{Title: "Test Book", Author: "Test Author"},
			TransferStatus: "transferred",
			MethodUsed:     "native-ssh",
			FilesCount:     1,
			SourceSize:     fi.Size(),
			SourceModTime:  fi.ModTime(),
		},
	}}
	path, _ := CheckpointPath()
	SaveCheckpoint(path, cp)

	report := RunTransfer(Config{SourceDir: src, DestDir: filepath.Join(home, "organized"),
		DryRun: true, Force: true})
	if report.Total != 1 || report.Resumed != 1 || report.Transferred != 0 {
		t.Fatalf("got Total=%d Resumed=%d Transferred=%d, want 1/1/0", report.Total, report.Resumed, report.Transferred)
	}
}
```

  Also assert the fast-path entry's result `Status == "resumed"` in `report.Results`.
- Dry-run counting (fresh book, no checkpoint): `RunTransfer` with `DryRun:true` → matched book gets `skipped` → `report.Skipped == 1`, `Transferred == 0`. (Extends the same test file.)
- Local-fallback counting: covered by the local-mode integration test in §3.5 (Issue 3), which asserts `report.Local == 1` with a real local copy.

---

## 3. Issue 3 — `--delete-source` (the main feature)

### 3.1 Safety policy (decided)

1. **Opt-in only** — `--delete-source` (short `-D`). Default off.
2. **Only after success** — a book is a deletion candidate iff its final `report.Results` status is `transferred` or `local` **after** the verify phase (verify flips failures to `failed`, so this also means "verified" when `--verify` is on). Failed / partial / unmatched / skipped / **resumed** books are never deleted.
3. **Never in dry-run** — deletion runs only in the real transfer phase (after verify). Dry-run prints the deletion plan but deletes nothing.
4. **Confirmation** — reuse the existing interactive prompt. In the `[3/5]` plan, print the exact deletion footprint per book, and when any deletion is planned change the prompt to `"Proceed with transfer and delete source files? (y/N): "`. `--force` (or non-interactive) skips the prompt. This matches the requirement "print exactly what will be deleted and require the same y/N confirmation as the transfer plan". No new external trash dependency (rejected `gio trash`/`trash-cli` — adds a system dep and is untestable/deterministic).
5. **Containment guards, always** — every target must be a strict child of `SourceDir` **and** must **not** be inside (or equal to) `DestDir` (DestDir defaults to `~/qbit/organized`, a child of `~/qbit` — deleting the organized copy is the cardinal sin).
6. **Local-only guard** — if `cfg.LocalOnly && samePath(SourceDir, DestDir)` → print `ERROR: --delete-source with --local requires SourceDir != DestDir; source deletion disabled for this run`, set `report.Failed++`, and delete nothing. (Fail the run, never half-delete.)
7. **Footprint precision** — see §3.2. Delete leaf book dirs only; never a shared container/author/series dir.

### 3.2 Footprint rules (`deleteFootprint`)

- **Directory book** (`!IsSingleFile && !IsFromZip`): delete the leaf **book directory** `book.Path`, but **only** if every `AudioFiles`/`CoverFiles` entry is inside it (defensive: if the scanner ever attributes a file outside `book.Path`, fall back to file-level deletion of just that book's own files). Scanner guarantees each directory-book owns a distinct leaf dir, so no sibling book shares it; the shared-container case only arises for single-file books (handled next).
- **Single-file book** (`IsSingleFile`): delete exactly the files in `book.AudioFiles` + `book.CoverFiles`, **plus** a same-directory cover image whose stem matches the audio stem (`Book.m4b` ↔ `Book.jpg`) — unambiguous ownership. Do **not** delete generic `cover.jpg` in a container dir (could be shared). Leave empty parent dirs behind (safe; optional cleanup follow-up).
- **Zip book** (`IsFromZip`): the extracted temp dir is already removed by scanner cleanup; local footprint is nothing. The source `.zip` is handled separately in §3.4.
- Every path goes through `isDeletionSafe` (strict-child-of-`SourceDir` and not-under/equal `DestDir`).

### 3.3 New file `pkg/organizer/delete_source.go` — function signatures

```go
package organizer

// isPathWithin reports whether p is a strict child of root (both absolute,
// cleaned). p == root or p outside root => false.
func isPathWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// samePath reports whether two paths resolve to the same cleaned absolute path.
func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// isDeletionSafe enforces the SourceDir strict-child AND DestDir exclusion
// guards for a single deletion target.
func isDeletionSafe(target string, cfg Config) bool {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	src, _ := filepath.Abs(cfg.SourceDir)
	if !isPathWithin(abs, src) {
		return false // outside SourceDir
	}
	if cfg.DestDir != "" {
		dst, _ := filepath.Abs(cfg.DestDir)
		if isPathWithin(abs, dst) || samePath(abs, dst) {
			return false // would delete the organized copy
		}
	}
	return true
}

// deleteFootprint returns the exact absolute paths to delete for one book,
// enforcing containment guards. Returns nil when there is nothing safe to delete.
func deleteFootprint(book *models.BookSource, cfg Config) []string

// coLocatedCovers returns same-dir cover images sharing the audio file's stem.
func coLocatedCovers(audioFile string) []string

// deletePaths removes each path (os.RemoveAll works for files and dirs).
func deletePaths(paths []string) error

// hasSuccessfulResult reports whether report has a transferred/local result for name.
func hasSuccessfulResult(name string, report *models.TransferReport) bool

// markSourceDeleted sets SourceDeleted and bumps report.Deleted.
func markSourceDeleted(name string, report *models.TransferReport)

// deleteSourceFiles deletes local sources for every successfully transferred
// book. Call only when cfg.DeleteSource && !cfg.DryRun, AFTER verifyTransfers.
func deleteSourceFiles(cfg Config, matched []bookWithID, report *models.TransferReport)

// deleteEligibleZips removes a source .zip only when every book extracted from
// it has a successful result. Inert today (see §0 adjacent bug) but safe.
func deleteEligibleZips(cfg Config, matched []bookWithID, report *models.TransferReport)
```

Delete-iteration strategy (avoids name-collision risk): iterate `matched` (real `BookSource` objects), check `hasSuccessfulResult(m.book.Name, report)`, compute `deleteFootprint(m.book, cfg)` and delete. Name matching is only used for the cosmetic `SourceDeleted`/counter, exactly like the existing checkpoint-save loop (`organizer.go:448-467`).

### 3.4 Wiring

**A. `cmd/audiotransfer/main.go`**

- Flags (near line 37, after `no-virus-scan`):

```go
	deleteSource := flag.Bool("delete-source", false, "Delete source files after successful transfer (destructive; requires confirmation unless --force)")
	deleteSourceShort := flag.Bool("D", false, "Delete source files after transfer (short)")
```

- Short-flag handling (with the other short flags, ~line 60):

```go
	if *deleteSourceShort {
		*deleteSource = true
	}
```

- Config literal (line 120 area): `DeleteSource: *deleteSource || *deleteSourceShort,`

**B. `pkg/organizer/organizer.go`**

- `Config` struct (line 26-42): add

```go
	DeleteSource bool // delete source files after successful transfer (opt-in; never in dry-run)
```

- `[3/5]` plan section (inside the `matched` loop, after the file-count print, line 281-282): when `cfg.DeleteSource && !cfg.DryRun`, compute and print the footprint:

```go
		if cfg.DeleteSource && !cfg.DryRun {
			if paths := deleteFootprint(m.book, cfg); len(paths) > 0 {
				fmt.Println("    → delete source after transfer:")
				for _, p := range paths {
					fmt.Printf("        %s\n", p)
				}
			}
		}
```

- Confirmation prompt (line 285-293): when `cfg.DeleteSource` and a footprint exists, use `"\n  Proceed with transfer and delete source files? (y/N): "`.
- Deletion phase — insert **after** the verify block (line 497-500) and **before** the failure recount (line 503):

```go
	// Phase 5b: delete successfully transferred source files (opt-in, never dry-run)
	if cfg.DeleteSource && !cfg.DryRun {
		deleteSourceFiles(cfg, matched, report)
	}
```

  Ordering is deliberate: checkpoint save happens in the transfer results loop (lines 447-468) **before** this point, so checkpoint entries store pre-deletion `SourceSize`/`SourceModTime`. After deletion the book won't re-appear in a scan (files gone); if the user re-downloads identical files later, the fast-path compares real sizes/mtimes (no zeros involved) and resumes correctly. `bookSourceStat` returning zeros is therefore never consulted for deleted books.

**C. `pkg/models/models.go`**

- `TransferResult` (line 75-83): add `SourceDeleted bool // true when --delete-source removed this book's local source`.
- `TransferReport`: add `Deleted int` (see §2.2A).
- `PrintSummary` (after `Unmatched`, ~line 109): print conditionally:

```go
	if r.Deleted > 0 {
		fmt.Printf("  Deleted (source removed) : %d\n", r.Deleted)
	}
```

**D. `pkg/scanner/scanner.go` + `pkg/models/models.go` (zip tracking)**

- `BookSource` (models.go:11-19): add `ZipPath string // source .zip this book was extracted from ("" when not a zip)`.
- `extractAndScanZip` (scanner.go:321-327): inside the `for _, b := range extracted` loop set `b.ZipPath = zipPath` alongside `b.IsFromZip = true`.

### 3.5 Tests — new file `pkg/organizer/delete_source_test.go` (package `organizer`)

Minimal cases required plus safety extras. Use `t.Setenv("HOME", t.TempDir())` and a `StubTransferClient`-style local mode where a real transfer is needed.

| # | Case | Setup / assertion |
|---|---|---|
| 1 | success → deleted | Local-only run (`cfg{LocalOnly:true, Force:true, DeleteSource:true, SourceDir:tmpA, DestDir:tmpB}`, single-file book `Test Author - Test Book.mp3`). `RunTransfer` → source file gone, dest copy present, `report.Local==1`, `report.Deleted==1`, `report.Results[0].SourceDeleted==true`. |
| 2 | failure → not deleted | Report has a `failed` result for the book; `deleteSourceFiles(cfg, matched, report)` → file still present, `report.Deleted==0`. |
| 3 | dry-run → not deleted | `cfg.DryRun:true` → `deleteSourceFiles` no-ops; also assert `deleteSourceFiles` early-returns. |
| 4 | single-file vs directory book | Directory book (audio under `SourceDir/Author/Book/`) deletes only the leaf `Book/` dir, leaves `Author/` and an unrelated sibling `Book2/` intact. Single-file book deletes only its own file; a second single-file book sharing the same container dir keeps its file. |
| 5 | co-located cover | `Book.m4b` + `Book.jpg` → both deleted; generic `cover.jpg` in the same dir is NOT deleted. |
| 6 | containment guard | `book.Path` outside `SourceDir` → not deleted. Target inside `DestDir` → not deleted. Target == `SourceDir` → not deleted (`isPathWithin` strict-child). |
| 7 | LocalOnly + SourceDir==DestDir | Refused: `report.Failed` incremented, nothing deleted, ERROR logged. |
| 8 | deletion disabled | `cfg.DeleteSource:false` → nothing deleted. |
| 9 | resumed not deleted | Matched book whose only result status is `resumed` → `hasSuccessfulResult` false → not deleted. |
| 10 | zip book | `deleteFootprint` returns nil for `IsFromZip`; `deleteEligibleZips` deletes the zip only when all its books have successful results and the zip passes `isDeletionSafe`. |
| 11 | unit: `isPathWithin` / `samePath` / `deleteFootprint` | Table-driven: `isPathWithin("/a/b", "/a")==true`, `("/a", "/a")==false`, `("/a/../b","/a")==false`, etc. |

Note: case 1 uses the real local `transfer.LocalClient` (no SSH needed) — it is also the Issue-2 "local-fallback counts correctly" verification.

---

## 4. Full file-change list

| File | Change | Issue |
|---|---|---|
| `pkg/virusscan/local.go` | `scanBatch` conditional flag; new `batchArgs` helper | 1 |
| `pkg/virusscan/scanner_test.go` | `TestScanBatchArgs_DaemonMode`, `TestScanBatchArgs_ClamscanMode`; keep existing method-name flexibility | 1 |
| `pkg/models/models.go` | `TransferResult.Status` comment + `SourceDeleted` field; `TransferReport.Resumed`, `.Deleted`; `PrintSummary` lines | 2, 3 |
| `pkg/organizer/organizer.go` | Fast-path → `resumed`; `resumedBooks` note; early-return condition; `Config.DeleteSource`; plan-section footprint print; combined prompt; deletion phase after verify | 2, 3 |
| `pkg/organizer/delete_source.go` | **New** — guards, footprint, delete helpers, zip deletion | 3 |
| `pkg/organizer/delete_source_test.go` | **New** — §3.5 cases | 3 |
| `pkg/organizer/organizer_test.go` | `TestCheckpointFastPathCountsResumed` + dry-run counting | 2 |
| `pkg/scanner/scanner.go` | Set `b.ZipPath` in `extractAndScanZip` | 3 |
| `cmd/audiotransfer/main.go` | `--delete-source` / `-D` flags + short-flag handling + Config wiring | 3 |

## 5. Build & verify commands

```bash
cd /home/jevonx/Projects/audioTransfer
gofmt -l pkg cmd              # expect: no output (or run `gofmt -w` on changed files)
go vet ./...                  # expect: clean
go build ./...                # expect: clean
go test ./...                 # expect: all pass
go test ./... -cover          # expect: >= 80% (deletion helpers are pure → easily covered)
```

Manual smoke (after build):
```bash
go run ./cmd/audiotransfer --source <copy-of-qbit> --dest <tmp-organized> \
  --delete-source --dry-run                # shows plan + footprints, deletes nothing
go run ./cmd/audiotransfer --source <copy> --dest <tmp> --local --delete-source --force
# confirm: source files removed, organized copy intact, summary shows Deleted (source removed)
```

## 6. Risks & rollback

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| 1 | **Irreversible source deletion** | Data loss if a book is deleted before the remote copy is durable | Opt-in flag; only `transferred`/`local` results (post-verify) qualify; exact-footprint plan printed + y/N confirmation (skip only with `--force`); never in dry-run; containment guards. Advise first run with `--dry-run` on a copy of `~/qbit`. |
| 2 | **Deleting the organized copy / sibling files** | DestDir loss; sibling book loss | `isDeletionSafe` excludes anything inside/equal `DestDir` (DestDir lives inside SourceDir by default); directory deletion is leaf-dir-only; single-file deletion is own-files + stem-matched cover only; shared-container dirs never removed. |
| 3 | **clamdscan regression** | Scan silently no-ops again | Fix is a 1-line conditional; both flag modes covered by unit tests; manual smoke on a clamd host. |
| 4 | **Resumed counting changes user-facing output** | Confusion if labels differ from expectation | Summary now reconciles (Transferred + Resumed + Local + Skipped + Failed + Unmatched ≈ Total); plan section prints the resumed list; label chosen to cover local-checkpoint entries. |
| 5 | **Zip books can't transfer (adjacent pre-existing bug)** | Zip deletion never fires; zip books already never transfer | Out of scope — `deleteEligibleZips` is safely gated (all-books-success), so it stays inert until the scanner temp-dir lifetime is fixed (follow-up: keep temp dir alive until transfer, or stream from zip). Flagged in §0. |
| 6 | **Checkpoint staleness after deletion** | Stale entries for deleted books | Harmless: book no longer scanned; re-downloaded identical files resume via fast-path with real sizes; checkpoint save ordering (before deletion) preserves pre-deletion stats. |
| 7 | **Verify interplay** | Deletion of a book whose remote copy actually failed | `verifyTransfers` flips failed verifications to `failed` before the deletion phase runs → those books are excluded. Deletion always runs after verify. |
| 8 | **Empty parent dirs left behind** | Minor clutter | Accepted (safety over tidiness). Optional follow-up: prune empty ancestor dirs up to (not including) SourceDir. |

**Rollback:** deletion is irreversible — the guarantee is prevention (guards/confirmation/dry-run), not undo. Code rollback is per-commit `git revert` (three logical commits: `fix:` clamdscan flag, `fix:` resumed counting, `feat:` delete-source + tests). Before enabling `--delete-source` for real, run once against a copy of `~/qbit` and confirm `--verify` + manual spot-check of the organized/remote copies.

---

## 7. Execution Status

**Status:** ✅ EXECUTED — all three issues implemented and verified.

- Issue 1 (clamdscan flag): `pkg/virusscan/local.go` `batchArgs` helper + `pkg/virusscan/scanner_test.go` tests. Verified with `go build`, `go vet`, `go test ./pkg/virusscan/`.
- Issue 2 (resumed counting): `TransferReport.Resumed`, fast-path emits `Status:"resumed"`, plan + summary reconcile. Tests: `TestCheckpointFastPathCountsResumed`, `TestDryRunCountsSkipped`.
- Issue 3 (`--delete-source`): `pkg/organizer/delete_source.go` (guards, footprint, zip deletion), wired into `organizer.go` Phase 5b (after verify, before failure recount), flags in `main.go`, `ZipPath` tracking in `scanner.go`/`models.go`. 11 test cases in `delete_source_test.go`.
- Full suite: `gofmt -l` clean on changed files, `go vet ./...` clean, `go build ./...` clean, `go test ./...` all pass.
- Manual smoke (local + `--delete-source --force`): source book dir deleted, organized copy intact, summary shows `Deleted (source removed): 1`.
