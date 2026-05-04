"""Multi-method file transfer to remote server or local copy."""
import os
import shutil
import socket
import subprocess
import sys
from pathlib import Path
from typing import List, Optional, Dict, Tuple

from .utils import logger

DEFAULT_HOST = "audiobookshelf"
DEFAULT_PORT = 22
DEFAULT_USER = "root"
DEFAULT_TARGET_BASE = "/audiobooks"

TRANSFER_METHODS = ["native-ssh", "local"]


class TransferError(Exception):
    pass


def format_size(size_bytes: int) -> str:
    for unit in ['B', 'KB', 'MB', 'GB']:
        if size_bytes < 1024:
            return f"{size_bytes:.1f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.1f} TB"


def _check_hostname_resolves(host: str, port: int = 22) -> Tuple[bool, str]:
    try:
        addrs = socket.getaddrinfo(host, port, socket.AF_UNSPEC, socket.SOCK_STREAM)
        if not addrs:
            return False, f"Cannot resolve hostname: {host}"
        ips = sorted(set(sockaddr[0] for _, _, _, _, sockaddr in addrs))
        return True, f"Resolved {host} to {', '.join(ips)}"
    except socket.gaierror:
        return False, f"Cannot resolve hostname: {host} (DNS failed — Tailscale running?)"
    except Exception as e:
        return False, f"Hostname check failed: {e}"


def _find_ssh_executable() -> Optional[Path]:
    if sys.platform == 'win32':
        ssh_path = Path(r"C:\Windows\System32\OpenSSH\ssh.exe")
        if ssh_path.exists():
            return ssh_path
    for name in ['ssh', 'ssh.exe']:
        for path_dir in os.environ.get('PATH', '').split(os.pathsep):
            candidate = Path(path_dir) / name
            if candidate.exists() and candidate.is_file():
                return candidate
    return None


def _find_scp_executable() -> Optional[Path]:
    if sys.platform == 'win32':
        scp_path = Path(r"C:\Windows\System32\OpenSSH\scp.exe")
        if scp_path.exists():
            return scp_path
    for name in ['scp', 'scp.exe']:
        for path_dir in os.environ.get('PATH', '').split(os.pathsep):
            candidate = Path(path_dir) / name
            if candidate.exists() and candidate.is_file():
                return candidate
    return None


def _escape_for_ssh(path: str) -> str:
    escaped = path.replace("'", "'\\''")
    return f"'{escaped}'"


def _validate_target_subpath(target_subpath: str) -> str:
    if not target_subpath:
        return ""
    if '..' in target_subpath:
        raise ValueError(f"Path traversal detected: {target_subpath}")
    if target_subpath.startswith('/'):
        target_subpath = target_subpath[1:]
    return target_subpath


# ============================================================
#  Method 1: Native SSH/SCP
# ============================================================

class NativeSSHTransferClient:
    def __init__(self, host: str = DEFAULT_HOST, port: int = DEFAULT_PORT,
                 user: str = DEFAULT_USER, target_base: str = DEFAULT_TARGET_BASE,
                 ssh_key_path: Optional[str] = None):
        self.host = host
        self.port = port
        self.user = user
        self.target_base = target_base.rstrip('/')
        self.ssh_key_path = ssh_key_path
        self._ssh_path: Optional[Path] = None
        self._scp_path: Optional[Path] = None

    @property
    def method_name(self) -> str:
        return "native-ssh"

    def _build_ssh_cmd(self, remote_command: str) -> List[str]:
        ssh = str(self._ssh_path or 'ssh')
        cmd = [
            ssh,
            "-o", "BatchMode=yes",
            "-o", "ConnectTimeout=10",
            "-o", "StrictHostKeyChecking=accept-new",
            "-o", "LogLevel=ERROR",
        ]
        if self.ssh_key_path:
            cmd.extend(["-i", str(self.ssh_key_path)])
        if self.port != 22:
            cmd.extend(["-p", str(self.port)])
        cmd.append(f"{self.user}@{self.host}")
        cmd.append(remote_command)
        return cmd

    def _build_scp_cmd(self, local: Path, remote_dir: str) -> List[str]:
        scp = str(self._scp_path or 'scp')
        cmd = [
            scp,
            "-o", "BatchMode=yes",
            "-o", "ConnectTimeout=10",
            "-o", "StrictHostKeyChecking=accept-new",
            "-o", "LogLevel=ERROR",
        ]
        if self.ssh_key_path:
            cmd.extend(["-i", str(self.ssh_key_path)])
        if self.port != 22:
            cmd.extend(["-P", str(self.port)])
        cmd.append(str(local))
        remote_part = _escape_for_ssh(f"{remote_dir.rstrip('/')}/")
        cmd.append(f"{self.user}@{self.host}:{remote_part}")
        return cmd

    def preflight(self) -> Tuple[bool, str]:
        self._ssh_path = _find_ssh_executable()
        self._scp_path = _find_scp_executable()
        if not self._ssh_path:
            return False, "ssh command not found. Install OpenSSH Client."
        if not self._scp_path:
            return False, "scp command not found."
        ok, msg = _check_hostname_resolves(self.host, self.port)
        return ok, msg

    def connect(self) -> bool:
        ok, msg = self.preflight()
        if not ok:
            logger.error(f"Pre-flight: {msg}")
            return False
        try:
            result = subprocess.run(
                self._build_ssh_cmd("echo ok"),
                capture_output=True, text=True, timeout=15,
            )
            if result.returncode == 0 and "ok" in result.stdout:
                logger.info(f"Connected to {self.user}@{self.host} via native SSH")
                return True
            logger.error(f"SSH failed (exit {result.returncode}): {result.stderr.strip()}")
            return False
        except Exception as e:
            logger.error(f"SSH connection error: {e}")
            return False

    def disconnect(self):
        pass

    def ensure_remote_dir(self, remote_path: str) -> bool:
        try:
            cmd = self._build_ssh_cmd(f"mkdir -p {_escape_for_ssh(remote_path)}")
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
            return result.returncode == 0
        except Exception as e:
            logger.error(f"Failed to create remote dir {remote_path}: {e}")
            return False

    def transfer_file(self, local_path: Path, remote_dir: str) -> bool:
        try:
            local_size = local_path.stat().st_size
        except (OSError, FileNotFoundError):
            logger.warning(f"  Cannot stat: {local_path}")
            return False

        logger.info(f"  Transferring: {local_path.name} ({format_size(local_size)})")
        try:
            cmd = self._build_scp_cmd(local_path, remote_dir)
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
            return result.returncode == 0
        except subprocess.TimeoutExpired:
            logger.error(f"  Transfer timed out: {local_path.name}")
            return False
        except Exception as e:
            logger.error(f"  Failed: {e}")
            return False

    def transfer_book(self, audio_files: List[Path], cover_files: List[Path],
                      target_subpath: str) -> bool:
        target_subpath = _validate_target_subpath(target_subpath)
        remote_dir = f"{self.target_base}/{target_subpath}"
        logger.info(f"  Target: {remote_dir}")

        if not self.ensure_remote_dir(remote_dir):
            return False

        all_files = [f for f in audio_files + cover_files if f.exists()]
        if not all_files:
            logger.warning("  No valid files to transfer")
            return False

        transferred = sum(1 for f in all_files if self.transfer_file(f, remote_dir))
        success = transferred == len(all_files)
        if success:
            logger.info(f"  OK: {transferred}/{len(all_files)} files")
        return success

    def verify_transfer(self, remote_subpath: str) -> Dict:
        result = {
            "path": f"{self.target_base}/{remote_subpath}",
            "exists": False, "files": [], "total_size": 0,
        }
        remote_path = f"{self.target_base}/{remote_subpath}"
        try:
            ls_result = subprocess.run(
                self._build_ssh_cmd(f"ls -la {_escape_for_ssh(remote_path)} 2>/dev/null || echo 'MISSING'"),
                capture_output=True, text=True, timeout=15,
            )
            if "MISSING" in ls_result.stdout or ls_result.returncode != 0:
                result["error"] = "Remote path not found"
                return result
            result["exists"] = True
            for line in ls_result.stdout.splitlines():
                if line.startswith('total') or line.startswith('d') or line.startswith('MISSING'):
                    continue
                parts = line.split()
                if len(parts) >= 9:
                    try:
                        size = int(parts[4])
                        result["files"].append({"name": parts[8], "size": size})
                        result["total_size"] += size
                    except (ValueError, IndexError):
                        pass
        except Exception as e:
            result["error"] = str(e)
        return result


# ============================================================
#  Method 2: Local file copy
# ============================================================

class LocalTransferClient:
    def __init__(self, target_base: str = ""):
        self.target_base = str(Path(target_base).resolve()) if target_base else ""

    @property
    def method_name(self) -> str:
        return "local"

    def preflight(self) -> Tuple[bool, str]:
        try:
            p = Path(self.target_base)
            p.mkdir(parents=True, exist_ok=True)
            (p / ".write_test").touch()
            (p / ".write_test").unlink()
            return True, f"Local directory ready: {self.target_base}"
        except Exception as e:
            return False, f"Cannot write to {self.target_base}: {e}"

    def connect(self) -> bool:
        ok, msg = self.preflight()
        if not ok:
            logger.error(f"Local pre-flight: {msg}")
            return False
        logger.info(f"Local target: {self.target_base}")
        return True

    def disconnect(self):
        pass

    def transfer_file(self, local_path: Path, remote_dir: str) -> bool:
        dest = Path(remote_dir) / local_path.name
        try:
            logger.info(f"  Copying locally: {local_path.name} ({format_size(local_path.stat().st_size)})")
            shutil.copy2(local_path, dest)
            return True
        except Exception as e:
            logger.error(f"  Failed to copy {local_path.name}: {e}")
            return False

    def transfer_book(self, audio_files: List[Path], cover_files: List[Path],
                      target_subpath: str) -> bool:
        target_subpath = _validate_target_subpath(target_subpath)
        local_dir = Path(self.target_base) / target_subpath
        logger.info(f"  Target: {local_dir}")

        local_dir.mkdir(parents=True, exist_ok=True)
        all_files = [f for f in audio_files + cover_files if f.exists()]
        if not all_files:
            logger.warning("  No valid files to copy")
            return False

        transferred = sum(1 for f in all_files if self.transfer_file(f, str(local_dir)))
        success = transferred == len(all_files)
        if success:
            logger.info(f"  OK: {transferred}/{len(all_files)} files")
        return success

    def verify_transfer(self, remote_subpath: str) -> Dict:
        local_path = Path(self.target_base) / remote_subpath
        result = {
            "path": str(local_path), "exists": local_path.exists(),
            "files": [], "total_size": 0,
        }
        if local_path.exists():
            for entry in local_path.iterdir():
                if entry.is_file():
                    sz = entry.stat().st_size
                    result["files"].append({"name": entry.name, "size": sz})
                    result["total_size"] += sz
        return result


# Transfer method registry
TRANSFER_CLIENTS = {
    "native-ssh": NativeSSHTransferClient,
    "local": LocalTransferClient,
}
