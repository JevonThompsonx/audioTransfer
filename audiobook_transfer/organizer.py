"""Orchestrates the full scan→parse→match→transfer pipeline."""
import shutil
import tempfile
from pathlib import Path
from typing import List, Optional

from .scanner import scan_directory
from .parser import parse_name
from .matcher import resolve_identity
from .models import BookSource, BookIdentity, TransferResult, TransferReport
from .transfer import (
    TRANSFER_CLIENTS, TRANSFER_METHODS,
    NativeSSHTransferClient, LocalTransferClient,
    DEFAULT_HOST, DEFAULT_TARGET_BASE,
    format_size,
)
from .utils import logger, sanitize_name

_persistent_tmp_dir: Optional[Path] = None


def _copy_temp_files_to_persistent(books: List[BookSource]) -> List[BookSource]:
    """Copy zip-extracted files from system temp to persistent location."""
    global _persistent_tmp_dir
    persistent_tmp = Path(tempfile.mkdtemp(prefix="abtransfer_persist_"))
    _persistent_tmp_dir = persistent_tmp
    logger.info(f"Persistent temp: {persistent_tmp}")

    new_books = []
    for book in books:
        new_book = BookSource(
            name=book.name, path=book.path,
            audio_files=list(book.audio_files),
            cover_files=list(book.cover_files),
            is_single_file=book.is_single_file,
            is_from_zip=book.is_from_zip,
        )
        needs_copy = any(
            str(tempfile.gettempdir()) in str(f.resolve())
            for f in book.audio_files + book.cover_files
        )
        if needs_copy and book.is_from_zip:
            try:
                for f in book.audio_files + book.cover_files:
                    if f.exists():
                        dest = persistent_tmp / sanitize_name(book.name) / f.name
                        dest.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copy2(f, dest)
                        if f in new_book.audio_files:
                            idx = new_book.audio_files.index(f)
                            new_book.audio_files[idx] = dest
                        if f in new_book.cover_files:
                            idx = new_book.cover_files.index(f)
                            new_book.cover_files[idx] = dest
                new_book.path = persistent_tmp / sanitize_name(book.name)
            except Exception as e:
                logger.warning(f"  Failed to copy temp files for {book.name}: {e}")
        new_books.append(new_book)
    return new_books


def _cleanup_persistent_temp():
    global _persistent_tmp_dir
    if _persistent_tmp_dir and _persistent_tmp_dir.exists():
        try:
            tmp_root = Path(tempfile.gettempdir()).resolve()
            if str(_persistent_tmp_dir.resolve()).startswith(str(tmp_root)):
                shutil.rmtree(_persistent_tmp_dir, ignore_errors=True)
        except Exception:
            pass


def _try_transfer_method(client, books_with_ids, report, method_name: str) -> bool:
    if not client.connect():
        logger.warning(f"  [{method_name}] Connection failed")
        return False

    try:
        any_success = False
        for book, identity in books_with_ids:
            existing = [r for r in report.results
                       if r.source_name == book.name and r.status in ('transferred', 'local')]
            if existing:
                continue

            print(f"\n  [{method_name}] {identity.author} / {identity.title}")
            try:
                success = client.transfer_book(
                    book.audio_files, book.cover_files, identity.target_path
                )
            except Exception as e:
                logger.error(f"  [{method_name}] Error: {e}")
                success = False

            is_local = method_name == "local"
            result = TransferResult(
                source_name=book.name, identity=identity,
                status=("local" if is_local else "transferred") if success else "failed",
                files_count=len(book.audio_files) + len(book.cover_files),
                error=None if success else f"[{method_name}] Transfer failed",
                method_used=method_name if success else None,
            )
            report.results = [r for r in report.results if r.source_name != book.name]
            report.results.append(result)

            if success:
                any_success = True
                if is_local:
                    report.local += 1
                else:
                    report.transferred += 1
        return any_success
    finally:
        client.disconnect()


def run_transfer(source_dir: Path, *, dry_run: bool = False,
                 interactive: bool = True, host: str = "audiobookshelf",
                 target_base: str = "/audiobooks", force: bool = False,
                 verify: bool = False, ssh_key_path: Optional[str] = None,
                 local_only: bool = False,
                 methods: Optional[List[str]] = None) -> TransferReport:
    """Run the full audiobook transfer pipeline."""
    report = TransferReport()

    logger.info(f"{'DRY RUN: ' if dry_run else ''}Starting transfer from {source_dir}")

    # Phase 1: Scan
    print(f"\n[1/4] Scanning {source_dir}...")
    books = scan_directory(source_dir)
    report.total = len(books)
    if not books:
        logger.warning("No audiobooks found")
        return report

    books = _copy_temp_files_to_persistent(books)

    # Phase 2: Parse + Match
    print(f"[2/4] Analyzing metadata for {len(books)} books...")
    identities: List[tuple] = []
    for i, book in enumerate(books, 1):
        print(f"\n  [{i}/{len(books)}] {book.name}")
        # Determine parent context
        if book.is_single_file:
            parent_name = book.path.name
        else:
            parent_name = book.path.parent.name
        # Skip source dir itself as parent context (eg "qbit" not an author)
        if parent_name == source_dir.name:
            parent_name = ""

        parsed = parse_name(book.name, parent_name=parent_name)

        # If parent is a series dir (Series (Author) pattern), inherit author/series
        if _is_series_pattern(parent_name):
            series_parsed = parse_name(parent_name, parent_name="")
            if series_parsed.author and not parsed.author:
                parsed.author = series_parsed.author
            if series_parsed.series and not parsed.series:
                parsed.series = series_parsed.series
            parsed.confidence = max(parsed.confidence, 60)
        identity = resolve_identity(parsed, interactive=interactive)
        if identity:
            identities.append((book, identity))
        else:
            result = TransferResult(source_name=book.name, status="unmatched")
            report.results.append(result)
            report.unmatched += 1

    if not identities:
        logger.warning("No books could be matched")
        return report

    # Phase 3: Confirm plan
    print(f"\n[3/4] Transfer plan ({len(identities)} books):")
    for book, identity in identities:
        print(f"  {identity.target_path}")
        print(f"    {len(book.audio_files)} audio, {len(book.cover_files)} covers")

    if not dry_run and not force and interactive:
        confirm = input(f"\n  Transfer {len(identities)} books to {host}:{target_base}? (y/N): ")
        if confirm.lower() != 'y':
            logger.info("Transfer cancelled")
            return report

    if dry_run:
        print(f"\n[4/4] DRY RUN — no files transferred")
        for book, identity in identities:
            result = TransferResult(
                source_name=book.name, identity=identity,
                status="skipped",
                files_count=len(book.audio_files) + len(book.cover_files),
            )
            report.results.append(result)
            report.skipped += 1
        report.print_summary()
        return report

    # Phase 4: Transfer
    print(f"\n[4/4] Transferring {len(identities)} books...")

    method_list = methods
    if not method_list:
        method_list = ["local"] if local_only else list(TRANSFER_METHODS)

    logger.info(f"Transfer chain: {' -> '.join(method_list)}")

    for method_name in method_list:
        client_class = TRANSFER_CLIENTS.get(method_name)
        if not client_class:
            logger.warning(f"Unknown method: {method_name}")
            continue

        report.methods_tried.append(method_name)

        if method_name == "local":
            client = client_class(target_base=target_base)
        else:
            client = client_class(
                host=host, target_base=target_base, ssh_key_path=ssh_key_path
            )

        any_success = _try_transfer_method(client, identities, report, method_name)

        if any_success:
            logger.info(f"  [{method_name}] Transferred some books")

        # All done?
        pending = sum(1 for r in report.results if r.status in ('pending', 'failed'))
        actually_failed = sum(1 for r in report.results
                             if r.status == 'failed'
                             and not any(r2.source_name == r.source_name
                                        and r2.status in ('transferred', 'local')
                                        for r2 in report.results))
        if actually_failed == 0 and pending == 0:
            logger.info("All books transferred!")
            break

    # Phase 5: Verify (if requested)
    if verify and not dry_run:
        print(f"\n[5/5] Verifying transfers...")
        _verify_transfers(report, host=host, target_base=target_base, ssh_key_path=ssh_key_path)

    _cleanup_persistent_temp()

    print(f"\n[DONE] Transfer complete!")
    report.print_summary()

    # Count failures (reset: verify already incremented)
    report.failed = 0
    for r in report.results:
        if r.status == 'failed':
            report.failed += 1

    if report.local > 0 and report.transferred == 0:
        print(f"\n  All books organized locally at: {target_base}")
        print(f"  Manual transfer: rsync -avzP {target_base}/ root@{host}:{target_base}/")

    return report


def _verify_transfers(report: TransferReport, host: str = DEFAULT_HOST, target_base: str = DEFAULT_TARGET_BASE, ssh_key_path: Optional[str] = None):
    """Verify transferred files exist on target."""
    for r in report.results:
        if r.status in ('transferred', 'local') and r.identity:
            if r.method_used and r.method_used in TRANSFER_CLIENTS:
                client_class = TRANSFER_CLIENTS[r.method_used]
                if r.method_used == "native-ssh":
                    client = client_class(host=host, target_base=target_base, ssh_key_path=ssh_key_path)
                else:
                    client = client_class(target_base=target_base)
                v = client.verify_transfer(r.identity.target_path)
            else:
                v = {"path": "", "exists": False, "files": [], "total_size": 0}

            if v.get("exists"):
                print(f"  OK: {v['path']} ({len(v['files'])} files, "
                      f"{format_size(v['total_size'])})")
            else:
                print(f"  MISSING: {v.get('path', r.identity.target_path)}")
                original_status = r.status
                r.status = "failed"
                r.error = f"Verification failed: {v.get('error', 'unknown')}"
                if original_status == 'transferred' and report.transferred > 0:
                    report.transferred -= 1
                elif original_status == 'local' and report.local > 0:
                    report.local -= 1
                report.failed += 1


def _is_series_pattern(name: str) -> bool:
    """Check if directory name follows 'Series Name (Author)' pattern."""
    last_open = name.rfind('(')
    if last_open < 0:
        return False
    last_close = name.rfind(')')
    if last_close <= last_open:
        return False
    before = name[:last_open].strip()
    return ' - ' not in before
