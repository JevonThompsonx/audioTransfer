"""Filename parser for audiobook naming patterns.
Combines regex patterns and heuristic matching from both projects.
"""
import re
from typing import Optional
from .models import ParsedInfo
from .utils import normalize_author, logger

# Regex patterns for structured audiobook naming conventions.
PATTERNS = [
    # Author - Series, Book N - Title
    (r'^(.+?)\s*[-–—]\s*(.+?),\s*Book\s*([\d.]+)\s*[-–—]\s*(.+)$',
     {'author': 1, 'series': 2, 'series_position': 3, 'title': 4, 'confidence': 90}),

    # Author - Series, Book N
    (r'^(.+?)\s*[-–—]\s*(.+?),\s*Book\s*([\d.]+)$',
     {'author': 1, 'series': 2, 'series_position': 3, 'confidence': 80}),

    # Author - Title [ASIN]
    (r'^(.+?)\s*[-–—]\s*(.+?)\s*\[([A-Z0-9]{10})\]$',
     {'author': 1, 'title': 2, 'asin': 3, 'confidence': 85}),

    # Word NN - Title (series with position, e.g. "Pern 01 - Dragonflight")
    (r'^([A-Za-z]+)\s+(\d+(?:\.\d+)?)\s+[-–—]\s+(.+)$',
     {'series': 1, 'series_position': 2, 'title': 3, 'confidence': 80}),

    # Author - Title (standard, lower confidence; requires space around dash)
    (r'^(.+?)\s+[-–—]\s+(.+)$',
     {'author': 1, 'title': 2, 'confidence': 70}),

    # [NN] Title (numbered series entry)
    (r'^\[(\d+)\]\s*(.+)$',
     {'series_position': 1, 'title': 2, 'confidence': 60}),

    # NN Title (no brackets, numbered series entry)
    (r'^(\d+)\s+(.+)$',
     {'series_position': 1, 'title': 2, 'confidence': 60}),

    # Title [ASIN]
    (r'^(.+?)\s*\[([A-Z0-9]{10})\]$',
     {'title': 1, 'asin': 2, 'confidence': 65}),

    # Just title (catch-all)
    (r'^(.+)$',
     {'title': 1, 'confidence': 30}),
]

# Words that indicate the string is NOT an author name.
NON_AUTHOR_WORDS = [
    'gothic horror', 'chapterized', 'series', 'unabridged', 'audiobook',
    'radio drama', 'book', 'entangled with fae', 'beauty and the beast',
    'retelling', 'abridged', 'dramatized', 'omnibus', 'collection',
    'edition', 'version', 'narrated', 'vol.', 'volume', 'trilogy',
    'saga', 'chronicles', 'cycle', 'anthology',
]

# Title indicator words for reverse pattern detection.
TITLE_WORDS = {'the', 'a', 'an', 'of', 'and', 'in', 'on', 'at', 'to',
               'for', 'with', 'from', 'by', 'or', 'it', 'is', 'no', 'not'}

AUDIO_EXTS = ['.m4b', '.m4a', '.mp3', '.aax', '.ogg', '.wma', '.flac', '.wav', '.aac', '.m4p', '.zip']
IMAGE_EXTS = ['.jpg', '.jpeg', '.png', '.gif', '.bmp', '.webp']


def strip_all_parens(s: str) -> str:
    """Remove all parenthetical groups from a string."""
    while True:
        start = s.find('(')
        if start < 0:
            break
        end = s.find(')', start)
        if end < 0:
            break
        s = s[:start] + s[end + 1:]
    return ' '.join(s.split())


def is_authorish(s: str) -> bool:
    """Heuristic: does this string look like a person's name?"""
    s = s.strip()
    if not s:
        return False
    words = s.split()
    if len(words) < 1 or len(words) > 5:
        return False
    for w in words:
        if len(w) > 15:
            return False
    lower = s.lower()
    for na in NON_AUTHOR_WORDS:
        if na in lower:
            return False
    return True


def is_author_name(s: str) -> bool:
    """Stricter check: is this string DEFINITELY an author name?
    Used for reverse pattern detection (Title - Author).
    """
    s = s.strip()
    if not s:
        return False
    words = s.split()
    if len(words) < 1 or len(words) > 4:
        return False
    lower = s.lower()
    if lower.startswith('the ') or lower.startswith('a ') or lower.startswith('an '):
        return False
    # Reject strings containing digits (not author names)
    for w in words:
        for c in w:
            if c.isdigit():
                return False
    title_word_count = sum(1 for w in words if w.lower() in TITLE_WORDS)
    if len(words) > 2 and title_word_count / len(words) > 0.3:
        return False
    for w in words:
        if len(w) > 15:
            return False
    return True


def is_title_like(s: str) -> bool:
    """Check if a string looks like a book title (not an author)."""
    s = s.strip()
    if not s:
        return False
    lower = s.lower()
    # Titles often start with articles
    if lower.startswith('the ') or lower.startswith('a ') or lower.startswith('an '):
        return True
    # Common title keywords
    title_keywords = [
        'chronicles', 'saga', 'trilogy', 'cycle', 'series',
        'of ', 'and ', 'in ', 'on ', 'at ', 'the ',
    ]
    keyword_count = sum(1 for kw in title_keywords if kw in lower)
    words = s.split()
    # Multi-word strings with many small words are likely titles
    if len(words) >= 4:
        return True
    # 3-word strings with at least one preposition/article likely title
    if len(words) == 3 and keyword_count >= 1:
        return True
    return keyword_count >= 2


def heuristic_parse(clean: str, info: ParsedInfo):
    """Heuristic parsing for non-standard patterns that regex misses.

    Handles:
    - "Series Name (Author)" — author in parenthetical
    - "Title - Author" — reverse pattern detection
    """
    # Pattern: "Series Name (Author)" — author in last parenthetical
    last_open = clean.rfind('(')
    if last_open >= 0:
        last_close = clean.rfind(')')
        if last_close > last_open:
            before = clean[:last_open].strip()
            paren_content = clean[last_open + 1:last_close].strip()

            before = strip_all_parens(before)

            if ' - ' not in before and is_authorish(paren_content):
                info.author = paren_content
                info.series = before
                info.confidence = max(info.confidence, 75)
                return
            else:
                clean = before
        else:
            clean = strip_all_parens(clean)
    else:
        clean = strip_all_parens(clean)

    # Pattern: "Author - Title" or "Title - Author"
    if ' - ' in clean:
        idx = clean.index(' - ')
        left = clean[:idx].strip()
        right = clean[idx + 3:].strip()

        # Check for reverse pattern (Title - Author)
        if is_author_name(right) and not is_author_name(left):
            info.author = right
            info.title = left
            info.confidence = max(info.confidence, 70)
            return

        # Standard: Author - Title
        info.author = left
        info.title = strip_all_parens(right)
        info.confidence = max(info.confidence, 65)
        return

    # Fallback: entire string as title
    if not info.title:
        info.title = clean
        if info.confidence < 30:
            info.confidence = 30


def regex_parse(clean: str, info: ParsedInfo):
    """Try structured regex patterns against the clean name."""
    for pattern, mapping in PATTERNS:
        match = re.match(pattern, clean, re.DOTALL)
        if not match:
            continue

        groups = match.groups()
        regex_conf = mapping.get('confidence', 0)

        # Only override heuristic results if regex confidence is higher
        if regex_conf > info.confidence:
            info.title = None
            info.author = None
            info.series = None
            info.series_position = None

        for field_name, group_idx in mapping.items():
            if field_name == 'confidence':
                info.confidence = max(info.confidence, regex_conf)
            elif group_idx - 1 < len(groups):
                value = groups[group_idx - 1].strip()
                if field_name == 'series_position':
                    try:
                        info.series_position = float(value)
                    except ValueError:
                        pass
                elif field_name == 'year':
                    try:
                        info.year = int(value)
                    except ValueError:
                        pass
                elif field_name == 'author':
                    if not info.author:
                        info.author = value
                elif field_name == 'title':
                    if not info.title:
                        info.title = value
                elif field_name == 'series':
                    if not info.series:
                        info.series = value
                elif field_name == 'asin':
                    if not info.asin:
                        info.asin = value
        break


def parse_parent_context(info: ParsedInfo, parent_name: Optional[str]):
    """Extract series name and author from parent directory name."""
    if not parent_name:
        return

    parent_clean = strip_all_parens(parent_name)

    # Try "Author - ..." pattern from parent for author
    if not info.author:
        m = re.match(r'^([^-–—]+?)\s*[-–—]\s+', parent_clean)
        if m:
            info.author = normalize_author(m.group(1).strip())
            info.confidence = max(info.confidence, 40)
            logger.debug(f"  Author from parent: '{info.author}'")

    # Try "Series (Author)" pattern from parent
    if not info.series or not info.author:
        last_open = parent_name.rfind('(')
        if last_open >= 0:
            last_close = parent_name.rfind(')')
            if last_close > last_open:
                before = parent_name[:last_open].strip()
                paren = parent_name[last_open + 1:last_close].strip()
                if is_authorish(paren) and ' - ' not in before:
                    if not info.author:
                        info.author = normalize_author(paren)
                        info.confidence = max(info.confidence, 50)
                    if not info.series and before:
                        info.series = before
                        info.confidence = max(info.confidence, 50)

    # Look for series keywords in parent name
    keyword_match = re.search(
        r'\s+(Series|Saga|Trilogy|Cycle|Chronicles|books\s+\d+)\b',
        parent_name, re.IGNORECASE
    )

    if keyword_match and not info.series:
        before = parent_name[:keyword_match.start()].strip()
        words = before.split()

        if words:
            # Don't extract author from parent if before starts with The/A/An
            lower_before = before.lower()
            if not info.author and len(words) >= 3 and len(words) <= 5 \
                    and not lower_before.startswith('the ') \
                    and not lower_before.startswith('a ') \
                    and not lower_before.startswith('an '):
                potential_author = ' '.join(words[:2])
                if is_author_name(potential_author):
                    info.author = potential_author
                    info.confidence = max(info.confidence, 35)
                    info.series = ' '.join(words[2:])
                    info.confidence = max(info.confidence, 40)
                    return

            # Fallback: use entire before as series
            info.series = before
            info.confidence = max(info.confidence, 40)
            logger.debug(f"  Series from parent: '{info.series}'")


def extract_asin(clean: str, info: ParsedInfo):
    """Find ASIN anywhere in the string."""
    if info.asin:
        return
    asin_match = re.search(r'\[([A-Z0-9]{10})\]', clean)
    if asin_match:
        info.asin = asin_match.group(1)
        info.confidence = max(info.confidence, 60)


def parse_name(name: str, parent_name: Optional[str] = None) -> ParsedInfo:
    """Parse an audiobook filename/directory name into metadata components.

    Uses a two-pass approach:
    1. Heuristic parsing (handles Series (Author), reverse patterns)
    2. Regex parsing (handles structured naming conventions)

    Args:
        name: The filename or directory name to parse
        parent_name: The parent directory name for additional context
    """
    clean = name.strip()

    # Strip audio/image extensions
    lower = clean.lower()
    for ext in AUDIO_EXTS + IMAGE_EXTS:
        if lower.endswith(ext):
            clean = clean[:-len(ext)]
            break

    info = ParsedInfo(raw_name=name)

    # Pass 1: Heuristic parsing
    heuristic_parse(clean, info)

    # Pass 2: Regex parsing (may override heuristic if higher confidence)
    regex_parse(clean, info)

    # Post-process: inherit author from parent if parent looks like author name
    if not info.author and parent_name:
        parent = parent_name
        author_to_use = parent_name
        if ',' in parent:
            parent = parent.split(',')[0].strip()
            author_to_use = parent
        if is_authorish(parent) and not is_title_like(parent):
            words = author_to_use.split()
            if len(words) <= 4:
                info.author = author_to_use
                info.confidence = max(info.confidence, 45)

    # Post-process: extract series position from Volume/Vol NN
    if info.series_position is None and info.title:
        vol_match = re.search(r'(?i)\s*(?:Volume|Vol\.?)\s*(\d+(?:\.\d+)?)', info.title)
        if vol_match:
            info.series_position = float(vol_match.group(1))
            info.title = (info.title[:vol_match.start()] + info.title[vol_match.end():]).strip()
            info.confidence = max(info.confidence, 55)
        else:
            bracket_match = re.search(r'(?i)\s*\[(?:Volume|Vol\.?)\s*(\d+(?:\.\d+))\]', info.title)
            if bracket_match:
                info.series_position = float(bracket_match.group(1))
                info.title = re.sub(r'(?i)\s*\[(?:Volume|Vol\.?)\s*\d+(?:\.\d+)\]', '', info.title).strip()
                info.confidence = max(info.confidence, 55)

    # Clean up title after volume removal
    if info.title:
        info.title = info.title.rstrip(' ,;.')
        info.title = re.sub(r'\[\s*\]', '', info.title).strip()
        info.title = re.sub(r'^\[[A-Z0-9]+\]\s*', '', info.title).strip()
        info.title = re.sub(r'(?i)\s*\((?:Audiobook|Unabridged|Unabr)\)', '', info.title).strip()
        info.title = re.sub(r'(?i)\s*\{[^}]*\}', '', info.title).strip()

    # Post-process: extract title from "Series NN - Title" pattern
    if not info.series and info.title:
        series_match = re.match(r'^(\w+)\s+(\d+(?:\.\d+)?)\s+[-–—]\s+(.+)$', info.title)
        if series_match:
            info.series = series_match.group(1)
            info.series_position = float(series_match.group(2))
            info.title = series_match.group(3)
            info.confidence = max(info.confidence, 65)

    # Post-processing
    if info.author:
        info.author = normalize_author(info.author)

    # Parent context
    parse_parent_context(info, parent_name)

    # ASIN extraction
    extract_asin(clean, info)

    logger.debug(f"Parsed '{name}' -> author={info.author}, title={info.title}, "
                 f"series={info.series}, pos={info.series_position}, conf={info.confidence}")
    return info
