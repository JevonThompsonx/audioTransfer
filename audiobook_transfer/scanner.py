"""Directory scanner for audiobook files."""
import zipfile
from pathlib import Path
from typing import List
from .models import BookSource
from .utils import is_audio, is_cover, is_zip, TempDir, sanitize_name, logger


def has_any_audio(dir_path: Path) -> bool:
    """Check if a directory tree contains any audio files."""
    for f in dir_path.rglob('*'):
        if f.is_file() and is_audio(f):
            return True
    return False


def is_series_dir(name: str) -> bool:
    """Check if directory name follows 'Series Name (Author)' pattern."""
    last_open = name.rfind('(')
    if last_open < 0:
        return False
    last_close = name.rfind(')')
    if last_close <= last_open:
        return False
    paren_content = name[last_open + 1:last_close].strip()
    if not paren_content:
        return False
    words = paren_content.split()
    if not (1 <= len(words) <= 4):
        return False
    before = name[:last_open].strip()
    if ' - ' in before:
        return False
    return True


def scan_directory(source_dir: Path, extract_zips: bool = True) -> List[BookSource]:
    """Scan a directory for audiobooks. Returns grouped book sources."""
    if not source_dir.exists() or not source_dir.is_dir():
        raise FileNotFoundError(f"Source directory not found: {source_dir}")

    logger.info(f"Scanning: {source_dir}")
    books: List[BookSource] = []

    skip_names = {'organized', 'organize-audiobooks', 'temp',
                  'audiobook_transfer', 'go-audiobook-transfer'}

    # Handle zip files
    if extract_zips:
        for zip_path in sorted(source_dir.glob('*.zip')):
            books.extend(_extract_and_scan_zip(zip_path))

    for entry in sorted(source_dir.iterdir()):
        name = entry.name
        if name in skip_names or name.startswith('.'):
            continue

        if entry.is_dir():
            books.extend(_scan_dir_entry(entry))
        elif is_audio(entry):
            book = BookSource(
                name=entry.stem,
                path=source_dir,
                audio_files=[entry],
                is_single_file=True,
            )
            books.append(book)
            logger.debug(f"  Book file: {entry.name}")

    logger.info(f"Found {len(books)} books")
    return books


def _scan_dir_entry(dir_path: Path) -> List[BookSource]:
    """Scan a single directory entry and categorize it."""
    name = dir_path.name
    books: List[BookSource] = []

    # Series (Author) pattern → treat as container with sub-books
    if is_series_dir(name):
        logger.debug(f"  Series dir: {name}")
        return _scan_container_dir(dir_path)

    # Collect all audio and cover files recursively
    audio_files = sorted([f for f in dir_path.rglob('*') if f.is_file() and is_audio(f)])
    cover_files = sorted([f for f in dir_path.rglob('*') if f.is_file() and is_cover(f)])

    if not audio_files:
        return books

    has_direct_audio = any(
        is_audio(f) for f in dir_path.iterdir() if f.is_file()
    )
    has_sub_books = any(
        e.is_dir() and has_any_audio(e)
        for e in dir_path.iterdir()
    )

    if (has_sub_books and not has_direct_audio) or (has_sub_books and len(audio_files) > 3):
        return _scan_container_dir(dir_path)

    # Flat book dir
    book = BookSource(
        name=name,
        path=dir_path,
        audio_files=audio_files,
        cover_files=cover_files,
    )
    books.append(book)
    logger.debug(f"  Book dir: {name} ({len(audio_files)} audio)")
    return books


def _scan_container_dir(dir_path: Path) -> List[BookSource]:
    """Scan a container directory with multiple sub-books."""
    books: List[BookSource] = []

    for entry in sorted(dir_path.iterdir()):
        name = entry.name
        if name.startswith('.'):
            continue

        if entry.is_dir() and has_any_audio(entry):
            sub_audio = sorted([f for f in entry.rglob('*') if f.is_file() and is_audio(f)])
            sub_covers = sorted([f for f in entry.rglob('*') if f.is_file() and is_cover(f)])
            book = BookSource(
                name=name,
                path=entry,
                audio_files=sub_audio,
                cover_files=sub_covers,
            )
            books.append(book)
            logger.debug(f"  Container book: {name} ({len(sub_audio)} files)")
        elif entry.is_file() and is_audio(entry):
            book = BookSource(
                name=entry.stem,
                path=dir_path,
                audio_files=[entry],
                is_single_file=True,
            )
            books.append(book)

    return books


def _extract_and_scan_zip(zip_path: Path) -> List[BookSource]:
    """Extract a zip file to temp dir and scan its contents."""
    logger.info(f"Extracting zip: {zip_path.name}")
    books = []
    try:
        with TempDir(prefix=f"zip_{zip_path.stem}_") as tmp:
            with zipfile.ZipFile(zip_path, 'r') as zf:
                zf.extractall(tmp)
            extracted = scan_directory(tmp, extract_zips=False)
            for b in extracted:
                b.is_from_zip = True
                if b.name != zip_path.stem:
                    b.name = f"{zip_path.stem}/{b.name}"
            books.extend(extracted)
    except zipfile.BadZipFile:
        logger.error(f"Corrupted zip file: {zip_path}")
    except Exception as e:
        logger.error(f"Failed to extract {zip_path.name}: {e}")
    return books
