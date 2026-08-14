# qBittorrent → Audiobookshelf automation (roadman)

Automated audiobook pipeline: qBittorrent runs on **roadman** (the Audiobookshelf
host), downloads are virus-scanned and organized into the ABS library, and
completed torrents are removed — no manual `audiotransfer` runs needed.

```
WebUI (any Tailscale device) ──> http://100.116.138.103:8081
        │  drop .torrent / magnet
        ▼
qbittorrent-nox (systemd, roadman)  downloads to /mnt/media/qbit
        │  torrent completes → "run external program" hook
        ▼
/usr/local/bin/qbit-postprocess.sh  (flock; waits until no torrents downloading)
        │  audiotransfer --source /mnt/media/qbit --local --dest /mnt/media/audiobooks --force --verify
        │    = ClamAV scan (clamd) → parse/match → organize Author/Title → chmod 755/644
        │    → verify → delete source → checkpoint
        ▼
files gone → qBittorrent API deletes the completed torrent
backstop: qbit-postprocess.timer (systemd, every 5 min) sweeps leftovers
```

## Components on roadman

| Piece | Location |
|---|---|
| qBittorrent service | `qbittorrent-nox@root.service` (Debian template unit, runs as root) |
| qBittorrent config | `/root/.config/qBittorrent/qBittorrent.conf` |
| WebUI credentials | `/root/.qbit-webui-password` (0600, `user=`/`pass=`) |
| Post-process wrapper | `/usr/local/bin/qbit-postprocess.sh` (root 700) |
| Post-process log | `/var/log/qbit-postprocess.log` |
| Backstop timer | `qbit-postprocess.timer` + `qbit-postprocess.service` (oneshot) |
| audiotransfer binary | `/usr/local/bin/audiotransfer` (static Go build) |
| Checkpoint / metadata cache | `/root/.audiotransfer/` |
| Download dir / temp | `/mnt/media/qbit` / `/mnt/media/qbit-temp` (temp is OUTSIDE the scanned dir) |
| ClamAV | `clamav-daemon` + `freshclam` (DBs auto-updated) |

## WebUI settings (applied via qBittorrent API + conf)

- Address `0.0.0.0`, port `8081`, username `admin`, generated password
  (`/root/.qbit-webui-password`).
- CSRF + clickjacking protection on; ban 5 failed logins / 3600 s; auth
  required even from localhost (`bypass_local_auth=false` — the API returns
  403 to unauthenticated local callers; the post-process wrapper logs in with
  the stored credentials).
- Save path `/mnt/media/qbit`; temp path `/mnt/media/qbit-temp`; incomplete
  files get `.!qB` suffix; preallocation on.
- Torrent peer port `15061`; anonymous mode; DHT/LSD/PeX off; encryption on.
- Completion hook (`autorun_program` + `autorun_enabled`):
  `/usr/local/bin/qbit-postprocess.sh "%N" "%F" "%I"`
- Proxy: **disabled**, credentials scrubbed. The flatpak's privado SOCKS5 creds
  were rejected by the proxy server ("User was rejected" from both aphrodite
  and roadman — creds look expired/rotated), so they were removed from the
  config entirely. To re-enable later: WebUI → Options → Connection → proxy,
  or
  `curl -b <cookie> -X POST http://127.0.0.1:8081/api/v2/app/setPreferences --data-urlencode 'json={"proxy_type":2,"proxy_ip":"...","proxy_port":1080,"proxy_username":"...","proxy_password":"...","proxy_auth_enabled":true}'`
  (verify egress first: `curl -x socks5h://user:pass@host:1080 https://api.ipify.org`).

## Virus scan behavior (fail closed)

The pre-transfer scan is **fail closed**: a book is blocked if any of its files
is infected OR could not be scanned (ClamAV error — permissions, daemon down,
unreadable file). Blocked books are logged (`INFECTED` / `SCAN ERROR (blocked)`
/ `SKIPPED (unsafe)`), never moved to the library, and their source files are
left in place for manual review. A scan infrastructure failure blocks all
books. This means if clamd is down, nothing transfers until it recovers (the
timer retries every 5 min) — safe by design.

## How the post-process works

1. qBittorrent fires the hook on torrent completion (needs the `autorun_enabled`
   preference — see note below).
2. The wrapper takes a `flock` (`/run/qbit-postprocess.lock`) — concurrent hooks
   skip, because one audiotransfer run covers the whole source dir.
3. It logs into the local WebUI API and **waits until no torrent is in a
   downloading state** (up to 30 min). This protects multi-file torrents that
   are still being written.
4. It runs audiotransfer with the pipeline defaulted on: ClamAV scan (clamd via
   `clamdscan`), identity match (OpenLibrary cached), organize into
   `/mnt/media/audiobooks/Author/Title`, chmod `755` dirs / `644` files, verify,
   **delete source**, checkpoint.
5. On success it deletes the completed torrent from qBittorrent
   (`deleteFiles=false` — files are already gone) **only if the torrent's
   content path no longer contains files**. Non-audiobook torrents (or books the
   scanner couldn't match) are left alone. A torrent whose files got flagged
   infected is also left in place for manual review.
6. The systemd timer re-runs the wrapper every 5 min as a backstop (missed
   hooks, restarts).

Audiobookshelf picks up new folders automatically (it watches the library bind
mount). If it doesn't appear within a minute or two, trigger a library scan from
the ABS UI.

## Operational notes

- **Add a torrent**: WebUI → green plus → upload `.torrent` or paste magnet.
  Downloads land in `/mnt/media/qbit`, get processed automatically, and the
  torrent disappears from the list when done.
- **Virus found**: book is skipped (logged `INFECTED` / `SKIPPED (infected)`),
  source file kept, torrent kept. Check `/var/log/qbit-postprocess.log`, deal
  with the file manually, and delete the torrent yourself.
- **Pipeline failed** (e.g. roadman offline, disk full, parse disaster): wrapper
  logs `audiotransfer exit=N` and leaves torrents in place; the timer retries.
  Check `/var/log/qbit-postprocess.log` and the checkpoint state.
- **Watch the logs**:
  `ssh roadman 'tail -f /var/log/qbit-postprocess.log /var/log/qbittorrent/*.log'`
- **Manual transfer still available**: the old aphrodite flow
  (`./audiotransfer --source ~/qbit`) is untouched and works in parallel —
  downloads made on aphrodite still transfer over SSH as before. (The aphrodite
  Flatpak qBittorrent also still has its stale privado proxy creds configured.)
- **Reset the WebUI password**:
  `ssh roadman` then `openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | head -c 24`
  and apply via API `setPreferences {"web_ui_password":"..."}` (or edit the
  PBKDF2 hash in the conf and restart). Keep `/root/.qbit-webui-password` in sync.

## Maintenance / failure-recovery

- qBittorrent restart: `systemctl restart qbittorrent-nox@root`
- Timer state: `systemctl list-timers qbit-postprocess.timer`
- Run the post-process once by hand (as root on roadman):
  `/usr/local/bin/qbit-postprocess.sh`
- Rebuild/redeploy audiotransfer after source changes (build on aphrodite,
  static for roadman):
  ```bash
  cd ~/Projects/audioTransfer
  CGO_ENABLED=0 go build -trimpath -o /tmp/audiotransfer-static ./cmd/audiotransfer/
  scp /tmp/audiotransfer-static roadman:/usr/local/bin/audiotransfer
  ```

## Security posture (see also the deployment review)

- qBittorrent runs as **root** (matches the rest of roadman: SSH root, all
  containers root). WebUI is password-protected with fail-ban, but it IS bound
  to `0.0.0.0` — keep the password strong and treat the LAN as semi-trusted.
  Optional hardening paths: run qbittorrent-nox under a dedicated user
  (service template supports `qbittorrent-nox@<user>`), or add a firewall rule
  restricting port 8081 to the tailnet
  (`nft ... tcp dport 8081 ip saddr 100.64.0.0/10 accept`), or front the WebUI
  with a TLS-terminating reverse proxy (caddy already runs on roadman).
- ClamAV AppArmor profile (`/etc/apparmor.d/usr.sbin.clamd`) was extended with
  read-only access to `/mnt/media` so clamd can scan downloads
  (`/mnt/media/ r,` + `/mnt/media/** r,`). Backup of the original:
  `/root/usr.sbin.clamd.bak`.
- ClamAV blocks encrypted archives (`ArchiveBlockEncrypted true` in
  `/etc/clamav/clamd.conf`) — password-protected zips in torrents are flagged
  (`Heuristics.Encrypted.Zip`) and fail closed.
- Localhost API access requires authentication (`bypass_local_auth=false`).
- The WebUI password is rotated on demand; currently in `/root/.qbit-webui-password`
  (0600). Rotate with:
  ```bash
  ssh roadman   # or ssh -J templetonbak roadman while the direct path is down
  # login with the current password, then:
  curl -s -b <cookie> -X POST http://127.0.0.1:8081/api/v2/app/setPreferences \
    --data-urlencode "json={\"web_ui_password\":\"NEWPASS\"}"
  ```
- WebUI serves plain HTTP (no TLS). Session cookies are NOT marked Secure
  (that would break HTTP logins); if you add TLS via a reverse proxy, also set
  `WebUI\SecureCookie=true`.
- qBittorrent API returns the configured proxy password in `preferences`; the
  proxy is currently disabled and its credentials were scrubbed, so nothing
  sensitive is exposed there.
- Torrent names/paths are sanitized (printable chars only, truncated) before
  being written to `/var/log/qbit-postprocess.log`.
