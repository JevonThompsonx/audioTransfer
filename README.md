# audioTransfer

Audiobook organizer and transfer tool for Audiobookshelf. Combines the best of two independent implementations.

**Two implementations available:** Go (actively maintained, recommended) and Python (legacy, frozen, not recommended for new use).

> **The Go implementation is the actively maintained version.** The Python implementation (`audiobook_transfer/`) is now legacy/frozen — it has known bugs (directory-tree scanning that can flatten nested series into one entry, author misattribution for books nested under series subfolders, and no resume/checkpoint support across interrupted runs) that have been fixed in the Go version but will not be ported back to Python. Use the Go version for all real usage; the Python code is kept only for reference.

## Quick Start

### Go (recommended — zero external dependencies)
```bash
go build -o audiotransfer ./cmd/audiotransfer/
./audiotransfer --source ~/qbit --dry-run      # preview only
./audiotransfer --source ~/qbit --local         # organize locally
./audiotransfer --source ~/qbit                 # organize + transfer via SSH
```

### Python
```bash
python3 -m audiobook_transfer.cli --source ~/qbit --dry-run
# or install:
pip install -e .
audiotransfer --source ~/qbit --dry-run
```

## Usage

```
audiotransfer [options]

Options:
  --source, -s     Source directory (default: ~/qbit)
  --host, -H       Remote hostname (default: roadman)
  --target, -t     Remote target path (default: /mnt/media/audiobooks)
  --ssh-key, -k    SSH private key path (auto-detected if unset)
  --parallel, -P   Max concurrent transfers (default: 2)
  --dry-run, -n    Preview plan without transferring
  --local, -L      Local copy only, no SSH
  --force, -f      Skip confirmation prompts
  --interactive, -i  Confirm each book match individually
  --verify, -V     Verify transfers after completion
  --verbose, -v    Debug output
  --methods, -m    Transfer methods in order: native-ssh,local
  --delete-source  Delete source files after successful transfer (default OFF; requires --verify)
  --keep-source, -K  Keep source files after transfer (disables --delete-source)
```

**Concurrency note:** The `--parallel` flag controls concurrent file transfers. SSH connection multiplexing (see "native-ssh" transfer method below) reuses a single authenticated connection for multiple transfers and verification checks, removing per-operation connection overhead. This makes running multiple transfers safely. However, concurrent large audiobook files still compete for the same upload bandwidth — higher values don't necessarily yield proportionally faster transfers. The conservative default of 2 is a reasonable starting point; users with faster/more reliable links can experiment with higher values up to 8.

### Examples

```bash
# Preview: check what would happen without touching anything
./audiotransfer --source ~/Downloads/audiobooks --dry-run

# Local only: organize files into Author/Series/Title structure locally
./audiotransfer --source ~/qbit --local --target ~/organized

# Full transfer: organize and send to server via SSH
./audiotransfer --source ~/qbit --host roadman --verify

# Interactive mode: confirm each book match
./audiotransfer --source ~/qbit --interactive

# Force mode: skip all prompts (for scripts)
./audiotransfer --source ~/qbit --force --local

# Keep source files on this device (deletion is off by default)
./audiotransfer --source ~/qbit --keep-source
```

> **Source deletion is opt-in and verify-gated:** `--delete-source` is OFF by default. When you enable it, you MUST also pass `--verify` — the tool refuses to delete source files that have not been verified on the target, so a book is never deleted on scp success alone (silent data loss). Use `--keep-source` to retain them, and run `--dry-run` first to preview exactly what would be deleted.

# Custom SSH key and port
./audiotransfer --source ~/qbit --ssh-key ~/.ssh/id_ed25519
```

## Architecture

```
Source dir ──→ Scan ──→ Parse ──→ [Provider chain] ──→ Match ──→ Transfer + metadata.json
                                    Audible → iTunes → OpenLibrary
```

### Pipeline Phases

| Phase | Description |
|-------|-------------|
| **1. Scan** | Recursively discovers audiobook files (.m4b, .mp3, .m4a, .flac, etc.), cover art (.jpg, .png), and extracts .zip archives |
| **2. Parse** | Hybrid regex + heuristic engine extracts author, title, series, series position from filenames and parent directories |
| **3. Match** | Resolves canonical book identity via the metadata provider chain (below) with strict scored matching |
| **4. Transfer** | Copies files to target via SSH/SCP (native) or local file copy; falls back through methods on failure |
| **5. metadata.json** | A full ABS-compatible `metadata.json` (title, authors, narrators, series, tags, genres, description, publisher, year, language, ISBN, ASIN) is written into every organized book folder so Audiobookshelf scans new books fully matched — no more empty tags/descriptions |

## Metadata Provider Chain

Every provider result is **scored** against the parsed title/author before being accepted (title token-overlap ≥ 0.6, author match ≥ 0.66 with comma/&/;-separated segment matching), and matching is **volume-aware** — a result that is a different volume of the same series (e.g. "Vol. 08" vs "Volume 17") is rejected. Results are cached 30 days.

| Provider | What it provides | Notes |
|----------|------------------|-------|
| **Audible** (catalog search → `audnex.us` per-ASIN enrichment) | Full metadata: title, subtitle, authors, narrators, publisher, description, release year, cover, tags + genres, primary + secondary series with positions, language, ISBN, ASIN | Primary — same source Audiobookshelf uses. audnex.us is a free community proxy, best-effort per ASIN |
| **iTunes** (Apple search, `media=audiobook`) | Title, author, description, year | Fallback — no tags/series data |
| **OpenLibrary** (search.json) | Title, author, year, cover | Last resort — sparse |

## Parser: Filename Patterns

### Target Structure

Before (flat source directory):
```
~/qbit/
  Author - Title.m4b
  Author - Title 2.m4b
  Series Name (Author)/
    Book One/
      files...
    Book Two/
      files...
```

After (organized):
```
/mnt/media/audiobooks/         ← host path bound into Audiobookshelf as /audiobooks
  Author/
    Title/
      Author - Title.m4b
    Title 2/
      Author - Title 2.m4b
    Series Name/
      Book One/
        files...
      Book Two/
        files...
```

## Parser: Filename Patterns

The hybrid parser handles these naming conventions:

| Pattern | Example | Detects |
|---------|---------|---------|
| `Author - Title` | `Tamsyn Muir - Princess Floralinda.m4b` | Author, Title |
| `Author - Series, Book N - Title` | `Brandon Sanderson - Stormlight, Book 1 - The Way of Kings.m4b` | Author, Series, Position, Title |
| `Author - Series, Book N` | `Robin Hobb - Farseer, Book 1.m4b` | Author, Series, Position |
| `Author - Title [ASIN]` | `Stephen King - IT [B012345678].m4b` | Author, Title, ASIN |
| `Title [ASIN]` | `The Shining [B012345678].m4b` | Title, ASIN |
| `[NN] Title` | `[03] Royal Assassin.m4b` | Position, Title |
| **Heuristic: `Series (Author)`** | `Realm of the Elderlings (Robin Hobb)/` | Author = Robin Hobb, Series = Realm of the Elderlings |
| **Heuristic: `Title - Author` (reverse)** | `The Shining - Stephen King.m4b` | Detects reverse pattern, assigns correctly |
| **`Series_Title -- Subtitle [ASIN]`** | `Embodied Activism_ Engaging the Body...--A Practical Guide... [B0BFJRTQNF].m4b` | Series = "Embodied Activism", Title parsed, ASIN extracted |
| **`Title [Series, Book N]`** | `Sweet Obsession [Dark Olympus Series, Book 8].m4b` | Title = "Sweet Obsession", Series = "Dark Olympus Series", Position = 8 (also handles `[Series, Book N - Subtitle]`) |
| **`Title Series, Book N`** (no brackets) | `House of Flame and Shadow Crescent City, Book 3.m4b` | Title = "House of Flame and Shadow", Series = "Crescent City", Position = 3 (conservative: series must be ≥2 words, no stopword edges) |

### Series Inheritance

When a directory follows the `Series (Author)` pattern, subdirectories automatically inherit the author and series:

```
Realm of the Elderlings (Robin Hobb)/     ← Author: Robin Hobb, Series: Realm of the Elderlings
  Assassin's Apprentice/                   ← Inherits: Robin Hobb / Realm of the Elderlings / Assassin's Apprentice
  Royal Assassin/                          ← Inherits: Robin Hobb / Realm of the Elderlings / Royal Assassin
```

## Transfer Methods

### native-ssh (preferred)
- Uses system `ssh`/`scp` commands
- Requires SSH key authentication (passwordless)
- Supports custom port via `-p` flag in system SSH config
- Sets `BatchMode=yes`, `ConnectTimeout=10`, `StrictHostKeyChecking=accept-new`
- Uses SSH connection multiplexing (ControlMaster/ControlPersist) so multiple file transfers and verification checks reuse one authenticated connection instead of reconnecting per operation — faster, and avoids remote SSH server connection-rate limits under heavy use (e.g. large libraries or high `--parallel` values)

### local (fallback)
- Copies files to the `--dest` directory (default `~/qbit/organized`)
- Preserves the same `Author/Series/Title` structure
- Always available — no dependencies
- Use `rsync` afterwards for manual transfer:
  ```bash
  rsync -avzP ~/qbit/organized/ root@roadman:/mnt/media/audiobooks/
  ```

> **Note:** The local method targets `--dest`, *not* the remote `--target` path. A previous bug passed the remote target (`/mnt/media/audiobooks`) to the local client, so SSH-fallback runs wrote the library into a local `/mnt/media/audiobooks` directory instead of `~/qbit/organized`. Fixed in `pkg/organizer/organizer.go`.

### Fallback Chain

```
native-ssh  ──→  local  (tried in order, stops when all books transferred)
```

## Resume & Checkpoints

The tool writes a checkpoint file to `~/.audiotransfer/checkpoint.json` after each successfully transferred book, recording its resolved identity and transfer status keyed by source file path. If a run is interrupted (crash, reboot, Ctrl-C) and relaunched against the same source directory, already-completed books are detected via this checkpoint and skipped entirely — no re-scanning, re-parsing, or re-querying metadata APIs for books already known to be done. The checkpoint also revalidates each entry's recorded file size and modification time against the live source file before trusting it, so replacing a source file's content (same path, different content) is correctly detected and re-processed rather than silently skipped.

OpenLibrary metadata lookups are cached persistently at `~/.audiotransfer/metadata_cache.json` (30-day TTL) — this makes repeated runs against the same library deterministic (the same book always resolves to the same identity, rather than potentially getting slightly different results from OpenLibrary's API on different runs).

## Project Structure

```
audioTransfer/
├── cmd/audiotransfer/main.go      Go CLI entry point
├── pkg/
│   ├── models/models.go           Shared data types (BookSource, ParsedInfo, BookIdentity, TransferReport)
│   ├── parser/parser.go           Hybrid filename parser (regex + heuristic)
│   ├── scanner/scanner.go         Recursive directory scanner with zip extraction
│   ├── metadata/metadata.go       OpenLibrary Search API client (cached, free, no key required)
│   ├── transfer/transfer.go       NativeSSHClient + LocalClient with fallback orchestration
│   ├── organizer/organizer.go     Pipeline orchestration (scan→parse→match→transfer)
│   └── utils/utils.go             File type helpers, temp dir, logging, path expansion
├── audiobook_transfer/            Python implementation — LEGACY, frozen, do not use for new work
│   ├── __init__.py
│   ├── cli.py                     argparse CLI with all flags
│   ├── models.py                  Dataclass types
│   ├── parser.py                  Hybrid parser (same logic as Go)
│   ├── scanner.py                 Directory scanner + zip extraction
│   ├── metadata.py                OpenLibrary API client
│   ├── matcher.py                 Identity resolution with interactive fallback
│   ├── transfer.py                NativeSSHTransferClient + LocalTransferClient
│   ├── organizer.py               Pipeline orchestration
│   └── utils.py                   File helpers, logging, temp dir, sanitize
├── go.mod
├── pyproject.toml
└── README.md
```

## Related Repos & Deployment

- `proxmox/audiobook-organizer/` is an **older, separate** Go organizer with
  similar naming; do not confuse the two. This repo (audioTransfer) is the
  canonical, actively maintained tool.
- Roadman production automation: `scripts/qbit-postprocess.sh` (deployed to
  `/usr/local/bin/` on roadman) chains ClamAV → audiotransfer → torrent
  cleanup; see `docs/ROADMAN_QBIT_AUTOMATION.md` for the full pipeline.
- Forgejo mirror: pushed through Aphrodite by the `forgejo-catchup` watchdog
  when skellyshome is back online; this checkout intentionally carries only
  `origin` (GitHub) plus an `aphrodite` fetch remote.

## Requirements

### Go version
- Go 1.21+
- Zero external Go dependencies (pure stdlib)
- `ssh` + `scp` in PATH (for remote transfer)
- Run the test suite with `go test ./...` (pkg/parser, pkg/organizer, pkg/scanner, pkg/metadata, pkg/transfer, pkg/virusscan all have unit tests)

### Python version
- Python 3.8+
- Zero required Python dependencies (pure stdlib)
- Optional: `paramiko` (`pip install -e ".[ssh]")` — not yet implemented

## Security Notes

- **No hardcoded secrets** — SSH keys via `--ssh-key` flag or auto-detected from `~/.ssh/`
- **Path traversal protection** — validates and sanitizes all paths before file operations
- **Zip slip protection** — extracted zip contents checked against temp directory boundaries
- **OpenLibrary API** — free, no API key required, read-only queries
- **Default user** — SSH defaults to `root`; configure via `--host` (e.g., `user@host` syntax in `~/.ssh/config`)
- **Host key checking** — uses `StrictHostKeyChecking=accept-new` on first connection; add host to `~/.ssh/known_hosts` before trusted use
- **File permissions** — transferred files/directories are set to `644`/`755` (owner read-write, group/other read-only) rather than world-writable. This was previously `777`; verified unnecessary since the reference Audiobookshelf deployment runs its container process as root (root bypasses Unix permission checks entirely), so tightening this is a pure security improvement with no functionality loss on that setup. If you're running Audiobookshelf as a non-root user via PUID/PGID and it can't read transferred files, you may need to adjust ownership/group membership on the target host rather than reverting to `777`.

## Limitations

- No audio tag reading (mutagen equivalent)
- No `paramiko` SSH backend implemented in Python version
- The Python implementation has no test suite; only the Go implementation is tested

> **Note:** the earlier "No tests yet" limitation is obsolete — the Go implementation now ships a unit-test suite across six packages (`go test ./...`).
