# Transfer Notes & Findings

## Connection Architecture

### Roadman Network
- **LAN IP:** `192.168.67.132` (wlan0, same subnet as aphrodite)
- **Tailscale IP:** `100.116.138.103`
- **SSH:** Port22, root login with pubkey auth (`PermitRootLogin prohibit-password`)
- **Tailscale SSH:** Enabled (handles auth via Tailscale identity, not standard keys)

### SSH Access
- **Tailscale SSH:** Works out of the box (Tailscale identity-based auth)
- **LAN SSH:** Requires aphrodite's pubkey in `~root/.ssh/authorized_keys` on roadman
  - Key was added: `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID1lYAOGtMnmHZ6phADQaNEnphStdDsTpeCaqADZKA8q opencode-diagnostics`
  - SSH config entry: `Host roadman` → `192.168.67.132`
  - **Note:** LAN SSH is intermittent — works for shell but file transfers hang. Use Tailscale IP for transfers.

### SSH Config (on aphrodite)
```
Host roadman
    HostName 192.168.67.132
    User root
    IdentityFile ~/.ssh/id_ed25519

Host roadman-ts
    HostName 100.116.138.103
    User root
    IdentityFile ~/.ssh/id_ed25519
```

## Transfer Performance

### Tailscale (current method)
- **Speed:** ~3-10 MB/s (varies, relay-dependent)
- **Connection:** Direct LAN path (`direct 192.168.67.132:41641`)
- **Reliability:** Single transfers work; **concurrent transfers cause SSH errors (code255)**
- **Solution:** Always serialize transfers (one at a time)

### LAN Direct (tested)
- **Shell SSH:** Works intermittently
- **File transfers (rsync/scp):** Hang after initial burst — likely Tailscale firewall interference
- **Recommendation:** Use Tailscale IP for now; LAN direct needs firewall debugging

### Transfer Rules
1. **NEVER run concurrent rsync/scp to roadman** — causes SSH mux failures
2. **Use `--inplace --no-compress`** flags with rsync for audio files
3. **Use `--timeout=30`** to detect stalled transfers
4. **Single file transfers complete reliably** (~2-5 min for700MB)

## Tool Bug Fixes

### Metadata Bug (FIXED)
**File:** `pkg/organizer/organizer.go` line640

**Problem:** When parser sets author from parent directory name (low confidence <=50), OpenLibrary's correct author was ignored because `identity.Author` was already set.

**Example:** `~/qbit/Red Rising/` → parser sets author="Red Rising" (conf45), OpenLibrary returns "Pierce Brown" but was ignored.

**Fix:** Changed condition from `if identity.Author == ""` to `if identity.Author == "" || identity.Confidence <=50`. Now OpenLibrary overrides low-confidence parent-dir guesses.

**Regression tests added:**
- `TestOpenLibraryOverridesLowConfidenceAuthor` — verifies Red Rising case
- `TestHighConfidenceAuthorNotOverridden` — verifies filename-parsed authors preserved

### Local Fallback Target Bug (FIXED)
**File:** `pkg/organizer/organizer.go`

**Problem:** The local transfer method was created with `cfg.TargetBase` (the remote target, `/mnt/media/audiobooks`) instead of `cfg.DestDir` (`~/qbit/organized`). When native-ssh failed and the tool fell back to `local`, it wrote the entire library into a **local** `/mnt/media/audiobooks` directory on the source machine (42GB on 2026-08-02) rather than into `~/qbit/organized`.

**Fix:** Both client-creation sites (`RunTransfer`, `verifyTransfers`) now pass `cfg.DestDir` when the method is `local`. The local-only rsync hint also references `DestDir`.

**Cleanup:** The erroneous local `/mnt/media/audiobooks` backup was verified file-for-file against the remote (all 160 files matched by name+size) and deleted on 2026-08-05.

## Roadman Library Fixes Applied

### Directory Structure Fixes
| Issue | Fix | Status |
|-------|-----|--------|
| `Red Rising/Red Rising/` (wrong author) | Moved to `Pierce Brown/Red Rising/` | ✅ |
| `Heavenly Tyrant/` (wrong author) | Renamed to `Xiran Jay Zhao/` | ✅ |
| `Vonnegut/` + `Kurt Vonnegut/` (duplicate) | Merged into `Kurt Vonnegut/` | ✅ |
| `Kumo Kagyu` loose files | Moved into book subdirs | ✅ |
| `Kumo Kagyu` duplicate Vol5 | Removed `Kumo-Kagyu-Goblin-Slayer-Vol-5/` | ✅ |
| `Richard Powers` flat (31 chapter files) | Moved into `The Overstory/` subdir | ✅ |
| `Sara Cate` loose file | Moved into `The Good Girl Effect/` | ✅ |
| `Edwin M. Griffiths` loose files | Moved into `The Newt and Demon/` | ✅ |
| `Sarah Rees Brennan` flat | Moved into `Long Live Evil/` | ✅ |
| `Max Monroe` flat | Moved into `Call Me Anytime/` | ✅ |
| `Ann Liang` flat | Moved into `A Song to Drown Rivers/` | ✅ |

### Transfers Completed
| Book | Size | Status |
|------|------|--------|
| Pierce Brown / Red Rising (2 parts) |1.5GB | ✅ |
| Kara A. Kennedy / I Will Never Leave You |275MB | ✅ |
| Aliette de Bodard / In the Vanishers' Palace (10 files+ cover) |188MB | ✅ |
| Xiran Jay Zhao / Iron Widow, Book 2 |231MB | ✅ |
| Laura Steven / Our Infinite Fates |611MB | ✅ |
| Brandon Sanderson / Mistborn series (9 books, ~26GB) |26GB | ✅ |
| Hannah Nicole Maehrer / Assistant to the Villain series (3 books) |~2GB | ✅ |
| Briar Boleyn / Bloodwing Academy (2 books) |~2GB | ✅ |
| Amber V. Nicole / Gods & Monsters (Book of Azrael 1-2, Wrath of the Fallen) |~2.7GB | ✅ |
| Rachel Gillig / One Dark Window + The Knight and the Moth |~1.1GB | ✅ |
| Roshani Chokshi / The Bronzed Beasts |680MB | ✅ |

### Truncated Transfers Recovered (2026-08-05)
| File | Was (bytes) | Now (bytes) | Fix |
|------|-------------|-------------|-----|
| Well of Ascension pt 2 |406,880,256 |815,513,760 | re-scp full file |
| Bloodwing Academy 01 |783,220,736 |1,120,144,597 | re-scp full file |
| Assistant to the Villain [Book 1] |198,049,792 |809,854,560 | re-scp full file |
| Iron Widow, Book 2 |241,255,233 |1,227,134,277 | re-scp full file |
| Wrath of the Fallen |768,049,152 |1,312,613,635 | re-scp from local fallback |
| The Bronzed Beasts |(missing) |680,253,994 | newly transferred |

### Known Issues Remaining
- **Missing covers:** Some books lack cover images (low priority)
- **`Lesbian Romance 27.07.2018`:**382 subdirs, flat dump structure (leave as-is unless requested)
- **`Fuse/That Time I Got Reincarnated as a Slime`:** Author "Fuse" is correct (it's the actual light novel author)
- **~95 stale `isMissing` items** (7 author-level + 88 Lesbian Romance parent-level): flagged during the 2026-08-05 forced rescan; **all purged** on 2026-08-05 via API (they had proper book-level replacements). Now 0 remaining.
- **Mistborn series description** set via ABS API (Era 1 + Era 2/Wax & Wayne + Secret History collection).

### Loose-File Merge Fixes (2026-08-05)
Nested books absorbing into one item due to loose audio at author/series level — fixed by moving nested book folders to siblings:

| Author | Books split | Result |
|--------|-------------|--------|
| Travis Baldree | `Legends and Lattes` + `Brigands & Breadknives` | 2 books (was 1 merged) |
| Shelby Mahurin | `Serpent & Dove` + `Blood & Honey` | 2 books (was 1 merged) |
| Colleen Hoover (German) | `Nur Noch Ein Einziges Mal` | 313 files (was 939 — triplicate extraction deduped, md5-verified identical) |

### Mistborn Series Reorganization (2026-08-05)
See `docs/MISTBORN_ORG_PLAN.md` for full details. Summary: 8 separate books in `Mistborn` series (Final Empire → Lost Metal → Secret History collection), correct sequence + author, covers fixed, track order fixed.

### Still Downloading (qBittorrent)
- `qbit/temp/` — `The Red Winter.m4b.!qB`, Gilded Wolves chapterized downloads (in-progress)

## Recommendations for Future Transfers

1. **Wait for downloads to complete** (`.!qB` files disappear)
2. **Run tool with fixed metadata** — `./audiotransfer --source ~/qbit/`
3. **Verify transfer plan** before confirming
4. **Transfer one book at a time** to avoid SSH failures
5. **Use `--verify` flag** to confirm remote files exist
6. **Check `~/.config/audioTransfer/checkpoint.json`** for transfer history

## Virus Scanning

### Setup
- **aphrodite:** ClamAV installed (`clamscan` at `/usr/bin/clamscan`)
- **roadman:** ClamAV installed + daemon running (`clamd` via systemd)
  - Install: `apt install clamav clamav-daemon`
  - Update: `freshclam`
  - Start: `systemctl enable --now clamav-daemon`

### Usage
```bash
# Default: virus scan enabled before transfer
./audiotransfer --source ~/qbit/

# Disable virus scan
./audiotransfer --source ~/qbit/ --no-virus-scan

# Dry run with scan preview
./audiotransfer --source ~/qbit/ --dry-run --virus-scan
```

### How It Works
1. **Pre-transfer scan (default on):** Scans all audio/cover files with ClamAV before transferring
2. **Infected files:** Books with infected files are skipped and reported
3. **Exit codes:** 0=clean, 1=infected found, 2=scanner error
4. **Performance:** Uses `clamdscan` (daemon) when available for faster bulk scanning

### Scan Existing Library on Roadman
```bash
# Run scan on roadman (background)
ssh root@roadman 'nohup clamscan -r --infected --no-summary --log=/var/log/clamav-library-scan.log /mnt/media/audiobooks/ > /dev/null 2>&1 &'

# Check progress
ssh root@roadman 'tail -5 /var/log/clamav-library-scan.log'

# Check for infections
ssh root@roadman 'grep FOUND /var/log/clamav-library-scan.log'
```

### Package Structure
```
pkg/virusscan/
├── scanner.go      # Scanner interface, ScanReport types
├── local.go        # LocalScanner (clamscan/clamdscan)
├── remote.go       # RemoteScanner (SSH to roadman)
├── factory.go      # NewScanner(mode, ...)
├── parsers.go      # ClamAV output parsing
└── scanner_test.go # Unit tests
```

## File Locations
- **Tool:** `/home/jevonx/Projects/audioTransfer/audiotransfer`
- **Source:** `/home/jevonx/qbit/`
- **Target:** `root@100.116.138.103:/mnt/media/audiobooks/`
- **Checkpoint:** `~/.audiotransfer/checkpoint.json`
- **Metadata cache:** `~/.audiotransfer/metadata_cache.json`
- **Roadman scan log:** `/var/log/clamav-library-scan.log`
