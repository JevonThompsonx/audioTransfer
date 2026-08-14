#!/usr/bin/env bash
# qbit-postprocess.sh - qBittorrent download-completion post-process (roadman)
#
# After torrents complete: waits until no torrent is downloading, then runs
# audiotransfer (ClamAV scan -> parse/match -> organize -> move into the
# Audiobookshelf library -> delete source), then removes completed torrents
# whose files are gone.
#
# Modes:
#   completion hook : qbit-postprocess.sh "%N" "%F" "%I"
#   backstop/timer  : qbit-postprocess.sh   (sweeps leftover completed torrents)
set -u

LOG=/var/log/qbit-postprocess.log
LOCK=/run/qbit-postprocess.lock
API=http://127.0.0.1:8081
REF="Referer: $API"
COOKIE=/tmp/qbit-pp-cookie.$$
TORRENTS=/tmp/qbit-pp-torrents.$$
CREDS=/root/.qbit-webui-password
AUDIOTRANSFER=/usr/local/bin/audiotransfer
SOURCE=/mnt/media/qbit
DEST=/mnt/media/audiobooks
LOGIN_RETRIES=5
IDLE_WAIT_MAX=1800
IDLE_POLL=30

DL_STATES="downloading|forcedDL|metaDL|forcedMetaDL|stalledDL|checkingDL|queuedDL|allocatingDL"

say() {
	# Sanitize before logging: torrent names/paths are attacker-influenced.
	local safe_msg
	safe_msg=$(printf '%s' "$*" | tr -cd '[:print:]\t' | head -c 512)
	echo "[$(date '+%F %T')] $safe_msg" >> "$LOG"
}

cleanup() { rm -f "$COOKIE" "$TORRENTS"; }
trap cleanup EXIT

# Single instance: skip when another run holds the lock (it covers all books).
exec 9>"$LOCK"
if ! flock -n 9; then
  say "skip: another run in progress"
  exit 0
fi

api_login() {
  local user pass
  user=$(sed -n 's/^user=//p' "$CREDS" 2>/dev/null | head -1)
  pass=$(sed -n 's/^pass=//p' "$CREDS" 2>/dev/null | head -1)
  [ -n "$user" ] && [ -n "$pass" ] || { say "ERROR: missing creds in $CREDS"; return 1; }
  curl -s -c "$COOKIE" -H "$REF" -X POST "$API/api/v2/auth/login" \
    --data-urlencode "username=$user" --data-urlencode "password=$pass" \
    | grep -q 'Ok\.'
}

api_get()  { curl -s -b "$COOKIE" -H "$REF" "$API$1"; }
api_post() { curl -s -b "$COOKIE" -H "$REF" -X POST "$API$1" "${@:2}"; }

# 0 = a torrent is actively downloading (or the API is unreachable/odd) -> keep waiting
any_downloading() {
  if ! api_get "/api/v2/torrents/info?filter=all" > "$TORRENTS" 2>/dev/null; then
    return 0
  fi
  python3 - "$TORRENTS" <<'PY'
import json, sys
try:
    t = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)  # unparseable -> assume downloading, retry
dl = {"downloading","forcedDL","metaDL","forcedMetaDL","stalledDL","checkingDL","queuedDL","allocatingDL"}
sys.exit(0 if any(x.get("state") in dl for x in t) else 1)
PY
}

torrent_hashes() {
  python3 - "$TORRENTS" <<'PY'
import json, sys
try:
    t = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
print(" ".join(x.get("hash","") for x in t))
PY
}

path_has_files() {
  [ -e "$1" ] || return 1
  if [ -d "$1" ]; then
    find "$1" -type f 2>/dev/null | grep -q . && return 0 || return 1
  fi
  return 0
}

delete_torrent() { # $1 = comma-separated hashes (verified present by caller)
  api_post "/api/v2/torrents/delete" --data-urlencode "hashes=$1" --data-urlencode "deleteFiles=false" >/dev/null
  say "removed torrent(s): $1"
}

main() {
  local name="${1:-}" content_path="${2:-}" hash="${3:-}"
  say "== post-process start (name='$name' path='$content_path' hash='$hash') =="

  # Authenticate (qBittorrent may be restarting -> retry)
  local i ok=0
  for i in $(seq 1 "$LOGIN_RETRIES"); do
    if api_login; then ok=1; break; fi
    say "login attempt $i/$LOGIN_RETRIES failed"
    [ "$i" -lt "$LOGIN_RETRIES" ] && sleep 5
  done
  if [ "$ok" -ne 1 ]; then
    say "ERROR: could not authenticate to qBittorrent API after $LOGIN_RETRIES tries"
    exit 1
  fi

  # Wait for downloads to idle (protects multi-file torrents mid-download)
  local waited=0
  while any_downloading; do
    waited=$((waited + IDLE_POLL))
    if [ "$waited" -ge "$IDLE_WAIT_MAX" ]; then
      say "timeout after ${IDLE_WAIT_MAX}s waiting for downloads; leaving torrents in place"
      exit 1
    fi
    sleep "$IDLE_POLL"
  done
  [ "$waited" -gt 0 ] && say "waited ${waited}s for downloads to idle"

  # Run the pipeline (virus scan on, organize, move, chmod, delete source)
  say "running: $AUDIOTRANSFER --source $SOURCE --local --dest $DEST --force --verify"
  "$AUDIOTRANSFER" --source "$SOURCE" --local --dest "$DEST" --force --verify >> "$LOG" 2>&1
  local rc=$?
  say "audiotransfer exit=$rc"
  if [ "$rc" -ne 0 ]; then
    say "pipeline failed; torrents left in place"
    exit 1
  fi

  # Refresh torrent list for cleanup decisions
  api_get "/api/v2/torrents/info?filter=all" > "$TORRENTS" 2>/dev/null || true
  local hashes
  hashes=$(torrent_hashes)

  # Never delete torrents based on "files missing" when the media filesystem
  # itself is unavailable (unmounted / mounting) — files are temporarily
  # invisible, not gone.
  if [ ! -d "$SOURCE" ]; then
    say "media dir '$SOURCE' not present (filesystem down?); skipping torrent cleanup"
    say "== post-process end =="
    exit 0
  fi

  if [ -n "$hash" ]; then
    # hook mode: remove THIS torrent if its files are gone (audiotransfer processed it)
    if ! path_has_files "$content_path"; then
      case " $hashes " in *" $hash "*) delete_torrent "$hash" ;; *) say "torrent $hash no longer listed; skip" ;; esac
    else
      say "files still present at '$content_path'; keeping torrent"
    fi
  else
    # backstop mode: sweep torrents that COMPLETED (progress >= 1.0) and whose
    # files no longer exist. Never delete a torrent that never finished
    # downloading (stalled/failed with 0% progress) — its files are "missing"
    # only because nothing was ever written.
    if [ -s "$TORRENTS" ]; then
      local h p prog
      while IFS=$'\t' read -r h p prog; do
        [ -z "$h" ] && continue
        # progress is a float like 0.0 or 1.0; only sweep fully-completed ones
        awk -v prog="$prog" 'BEGIN{exit !(prog >= 0.999)}' || continue
        if ! path_has_files "$p"; then
          case " $hashes " in *" $h "*) delete_torrent "$h" ;; esac
        fi
      done < <(python3 -c '
import json, sys
try:
    t = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for x in t:
    print(x.get("hash", "") + "\t" + str(x.get("content_path", "")) + "\t" + str(x.get("progress", 0)))
' < "$TORRENTS")
    fi
  fi

  say "== post-process end =="
}

main "$@"
