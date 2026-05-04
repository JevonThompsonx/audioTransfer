# audioTransfer

Audiobook organizer and transfer tool for Audiobookshelf. Combines the best of two independent implementations.

**Two implementations available:** Go (recommended for speed, zero deps) and Python.

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
  --host, -H       Remote hostname (default: audiobookshelf)
  --target, -t     Remote target path (default: /audiobooks)
  --ssh-key, -k    SSH private key path (auto-detected if unset)
  --dry-run, -n    Preview plan without transferring
  --local, -L      Local copy only, no SSH
  --force, -f      Skip confirmation prompts
  --interactive, -i  Confirm each book match individually
  --verify, -V     Verify transfers after completion
  --verbose, -v    Debug output
  --methods, -m    Transfer methods in order: native-ssh,local
```

### Examples

```bash
# Preview: check what would happen without touching anything
./audiotransfer --source ~/Downloads/audiobooks --dry-run

# Local only: organize files into Author/Series/Title structure locally
./audiotransfer --source ~/qbit --local --target ~/organized

# Full transfer: organize and send to server via SSH
./audiotransfer --source ~/qbit --host audiobookshelf --verify

# Interactive mode: confirm each book match
./audiotransfer --source ~/qbit --interactive

# Force mode: skip all prompts (for scripts)
./audiotransfer --source ~/qbit --force --local

# Custom SSH key and port
./audiotransfer --source ~/qbit --ssh-key ~/.ssh/id_ed25519
```

## Architecture

```
Source dir ──→ Scan ──→ Parse ──→ [OpenLibrary API] ──→ Match ──→ Transfer
                                                         │
                                                         ├── native-ssh (scp)
                                                         └── local (file copy)
```

### Pipeline Phases

| Phase | Description |
|-------|-------------|
| **1. Scan** | Recursively discovers audiobook files (.m4b, .mp3, .m4a, .flac, etc.), cover art (.jpg, .png), and extracts .zip archives |
| **2. Parse** | Hybrid regex + heuristic engine extracts author, title, series, series position from filenames and parent directories |
| **3. Match** | Resolves canonical book identity; optionally enriches via OpenLibrary API |
| **4. Transfer** | Copies files to target via SSH/SCP (native) or local file copy; falls back through methods on failure |

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
/audiobooks/
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

### local (fallback)
- Copies files to a local directory
- Preserves the same `Author/Series/Title` structure
- Always available — no dependencies
- Use `rsync` afterwards for manual transfer:
  ```bash
  rsync -avzP ~/qbit/organized/ root@audiobookshelf:/audiobooks/
  ```

### Fallback Chain

```
native-ssh  ──→  local  (tried in order, stops when all books transferred)
```

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
├── audiobook_transfer/            Python implementation (mirror of Go packages)
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

## Requirements

### Go version
- Go 1.21+
- Zero external Go dependencies (pure stdlib)
- `ssh` + `scp` in PATH (for remote transfer)

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

## Limitations

- No tests yet (test suite planned)
- No audio tag reading (mutagen equivalent)
- No resume/partial-transfer tracking
- Single-threaded transfers (no parallel SCP)
- No `paramiko` SSH backend implemented in Python version
