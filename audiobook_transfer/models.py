"""Shared data types for the audiobook transfer tool."""
from dataclasses import dataclass, field
from typing import Optional, List, Dict
from pathlib import Path


@dataclass
class BookSource:
    """One audiobook source (directory or single file)."""
    name: str
    path: Path
    audio_files: List[Path] = field(default_factory=list)
    cover_files: List[Path] = field(default_factory=list)
    is_single_file: bool = False
    is_from_zip: bool = False


@dataclass
class ParsedInfo:
    """Parsed metadata from filename/directory."""
    raw_name: str
    title: Optional[str] = None
    author: Optional[str] = None
    series: Optional[str] = None
    series_position: Optional[float] = None
    asin: Optional[str] = None
    narrator: Optional[str] = None
    year: Optional[int] = None
    confidence: int = 0
    extra: Dict = field(default_factory=dict)


@dataclass
class BookIdentity:
    """Final resolved book identity for transfer."""
    title: str
    author: str
    series: Optional[str] = None
    series_position: Optional[float] = None
    confidence: int = 0
    metadata_sources: List[str] = field(default_factory=list)

    def __post_init__(self):
        self.confidence = min(self.confidence, 100)

    @property
    def target_path(self) -> str:
        from .utils import sanitize_name
        author_dir = sanitize_name(self.author)
        book_dir = sanitize_name(self.title)
        if self.series:
            series_dir = sanitize_name(self.series)
            return f"{author_dir}/{series_dir}/{book_dir}"
        return f"{author_dir}/{book_dir}"


@dataclass
class TransferResult:
    """Result of transferring one book."""
    source_name: str
    identity: Optional[BookIdentity] = None
    status: str = "pending"
    error: Optional[str] = None
    files_count: int = 0
    total_bytes: int = 0
    method_used: Optional[str] = None


@dataclass
class TransferReport:
    """Summary of a transfer session."""
    total: int = 0
    transferred: int = 0
    skipped: int = 0
    failed: int = 0
    unmatched: int = 0
    local: int = 0
    results: List[TransferResult] = field(default_factory=list)
    methods_tried: List[str] = field(default_factory=list)

    def print_summary(self):
        print(f"\n{'='*60}")
        print(f"  TRANSFER SUMMARY")
        print(f"{'='*60}")
        print(f"  Total books scanned : {self.total}")
        print(f"  Transferred (remote): {self.transferred}")
        print(f"  Copied (local)      : {self.local}")
        print(f"  Skipped             : {self.skipped}")
        print(f"  Failed              : {self.failed}")
        print(f"  Unmatched           : {self.unmatched}")
        if self.methods_tried:
            print(f"  Transfer methods    : {' -> '.join(self.methods_tried)}")
        print(f"{'='*60}")

        if self.failed > 0:
            print(f"\n  Failed transfers:")
            for r in self.results:
                if r.status == 'failed':
                    print(f"    - {r.source_name}: {r.error}")
