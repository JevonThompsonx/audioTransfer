"""Match parsed info with API metadata to produce canonical book identity."""
from typing import Optional, List
from .models import ParsedInfo, BookIdentity
from .metadata import lookup as ol_lookup
from .utils import normalize_author, logger


def resolve_identity(parsed: ParsedInfo,
                     interactive: bool = True,
                     lookup_metadata: bool = True) -> Optional[BookIdentity]:
    """Resolve book identity from parsed info + optional API lookups."""
    sources: List[str] = ["filename"] if (parsed.author or parsed.title) else []
    author = parsed.author
    title = parsed.title
    series = parsed.series
    series_pos = parsed.series_position
    confidence = parsed.confidence

    # If we got a series but no author (Series (Author) pattern), author is the series dir
    if not author and series:
        title = series
        series = None
        confidence = max(confidence, 50)

    # Open Library API enrichment
    if lookup_metadata:
        search_title = title or series or parsed.raw_name
        search_author = author

        if search_title or search_author:
            ol = ol_lookup(title=search_title or "", author=search_author or "")
            if ol:
                sources.append("openlibrary")
                confidence += 15

                if not author and ol.author:
                    author = normalize_author(ol.author)
                if not title and ol.title:
                    title = ol.title
                if ol.year:
                    confidence += 5

                if search_title and ol.title:
                    if search_title.lower() not in ol.title.lower():
                        confidence -= 10

    # Interactive fallback for missing fields
    if not author and interactive:
        author = _prompt(f"Author for '{title or parsed.raw_name}':")
        if author:
            sources.append("manual")
            confidence = 60
        else:
            author = "Unknown"
            confidence = 5

    if not title and interactive:
        title = _prompt(f"Title for '{parsed.raw_name}':")
        if title:
            sources.append("manual")
            confidence = 60
        else:
            title = parsed.raw_name
            confidence = 5

    # Final fallbacks
    if not title:
        title = parsed.raw_name
    if not author:
        author = "Unknown"

    identity = BookIdentity(
        title=title.strip(),
        author=author.strip(),
        series=series.strip() if series else None,
        series_position=series_pos,
        confidence=min(confidence, 100),
        metadata_sources=sources,
    )

    logger.info(f"  Resolved: {identity.author} / {identity.series or '-'} / "
                f"{identity.title} (conf: {identity.confidence}%)")

    # Interactive confirmation for low confidence
    if interactive and confidence < 50:
        print(f"\n  Low confidence match ({confidence}%):")
        print(f"    Author : {identity.author}")
        print(f"    Title  : {identity.title}")
        if identity.series:
            print(f"    Series : {identity.series}")
        action = _prompt("  Accept? (y/n/edit)", default="y")
        if action.lower() == 'n':
            logger.warning(f"  Skipped: {parsed.raw_name}")
            return None
        elif action.lower() == 'e':
            new_author = _prompt("    Author:", default=identity.author)
            new_title = _prompt("    Title:", default=identity.title)
            new_series = _prompt("    Series:", default=identity.series or '')
            identity.author = new_author.strip() or identity.author
            identity.title = new_title.strip() or identity.title
            identity.series = new_series.strip() or None
            identity.confidence = 70
            identity.metadata_sources.append("manual-edit")

    return identity


def _prompt(question: str, default: str = '') -> str:
    """Prompt user for input with optional default."""
    if default:
        result = input(f"{question} [{default}]: ").strip()
        return result if result else default
    return input(f"{question} ").strip()
