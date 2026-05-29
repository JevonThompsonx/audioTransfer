"""Shared utilities for the audiobook transfer tool."""
import logging
import os
import sys
import tempfile
import shutil
from pathlib import Path
from typing import Optional

LOG_FORMAT = '%(asctime)s [%(levelname)s] %(message)s'
LOG_DATE_FORMAT = '%H:%M:%S'


def setup_logging(verbose: bool = False, log_file: Optional[str] = None):
    level = logging.DEBUG if verbose else logging.INFO
    handlers = [logging.StreamHandler(sys.stdout)]
    if log_file:
        handlers.append(logging.FileHandler(log_file, encoding='utf-8'))
    logging.basicConfig(level=level, format=LOG_FORMAT, datefmt=LOG_DATE_FORMAT, handlers=handlers)
    logging.getLogger('paramiko').setLevel(logging.WARNING)


logger = logging.getLogger('audiobook_transfer')

UNSAFE_CHARS = '<>:"/\\|?*'


def sanitize_name(name: str, max_len: int = 200) -> str:
    """Sanitize a name for use as a directory path component."""
    for ch in UNSAFE_CHARS:
        name = name.replace(ch, ' ')
    name = name.replace('..', '_')
    name = name.strip('. ')
    while '  ' in name:
        name = name.replace('  ', ' ')
    name = name[:max_len]
    name = name.rstrip('. ')
    return name if name else 'Unknown'


def normalize_author(name: str) -> str:
    """Normalize author: 'Last, First' -> 'First Last'
    Preserves multi-author format: 'Caroline Peckham, Susanne Valenti' stays unchanged.
    """
    name = name.strip()
    if ',' in name:
        parts = [p.strip() for p in name.split(',')]
        if len(parts) == 2:
            first, second = parts[0], parts[1]
            # "Last, First" pattern: single word before comma
            if len(first.split()) == 1 and 1 <= len(second.split()) <= 2:
                return f'{second} {first}'
            # Multi-author: keep as-is
            return name
    return name


AUDIO_EXT = {'.m4b', '.m4a', '.mp3', '.aax', '.ogg', '.wma', '.flac', '.wav', '.aac', '.m4p'}
COVER_EXT = {'.jpg', '.jpeg', '.png', '.gif', '.bmp', '.webp'}


def is_audio(path: Path) -> bool:
    return path.suffix.lower() in AUDIO_EXT


def is_cover(path: Path) -> bool:
    return path.suffix.lower() in COVER_EXT


def is_zip(path: Path) -> bool:
    return path.suffix.lower() == '.zip'


def must_expand(path: str) -> str:
    """Expand ~/ to user's home directory."""
    if path.startswith('~/'):
        return os.path.join(os.path.expanduser('~'), path[2:])
    return path


class TempDir:
    def __init__(self, prefix: str = 'abtransfer_'):
        self.path: Optional[Path] = None
        self.prefix = prefix

    def __enter__(self):
        self.path = Path(tempfile.mkdtemp(prefix=self.prefix))
        return self.path

    def __exit__(self, *args):
        if self.path and self.path.exists():
            shutil.rmtree(self.path, ignore_errors=True)
