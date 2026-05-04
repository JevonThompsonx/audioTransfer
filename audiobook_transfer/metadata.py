"""Open Library API enrichment for audiobook metadata."""
import json
import time
import urllib.request
import urllib.parse
from typing import Optional
from dataclasses import dataclass
from .utils import logger


@dataclass
class BookMetadata:
    """Enriched book metadata from APIs."""
    title: str = ""
    author: str = ""
    series: str = ""
    series_position: float = 0.0
    year: int = 0
    description: str = ""
    cover_url: str = ""
    ol_work_key: str = ""
    ol_author_key: str = ""
    confidence: int = 0
    source: str = ""


_cache: dict = {}


def lookup(title: str, author: str = "") -> Optional[BookMetadata]:
    """Search Open Library for book metadata. Cache results for 1 hour."""
    cache_key = f"{title.lower().strip()}|{author.lower().strip()}"

    if cache_key in _cache:
        entry = _cache[cache_key]
        if time.time() - entry['ts'] < 3600:
            logger.debug(f"Cache hit for '{cache_key}'")
            return entry['data']

    result = _search_open_library(title, author)
    _cache[cache_key] = {'data': result, 'ts': time.time()}
    return result


def _search_open_library(title: str, author: str = "") -> Optional[BookMetadata]:
    """Query Open Library search API."""
    params = urllib.parse.urlencode({
        'q': f"{title} {author}".strip(),
        'limit': '3',
        'fields': 'title,author_name,first_publish_year,key,author_key,cover_i',
    })
    url = f"https://openlibrary.org/search.json?{params}"
    logger.debug(f"OpenLibrary API: {url}")

    try:
        req = urllib.request.Request(url, headers={'User-Agent': 'audioTransfer/2.0'})
        with urllib.request.urlopen(req, timeout=15) as resp:
            data = json.loads(resp.read().decode())
    except Exception as e:
        logger.warning(f"OpenLibrary request failed: {e}")
        return None

    docs = data.get('docs', [])
    if not docs:
        return None

    doc = docs[0]

    meta = BookMetadata(
        title=doc.get('title', ''),
        author=', '.join(doc.get('author_name', [])),
        year=doc.get('first_publish_year', 0),
        ol_work_key=doc.get('key', ''),
        source='openlibrary',
    )

    author_keys = doc.get('author_key', [])
    if author_keys:
        meta.ol_author_key = author_keys[0]

    cover_i = doc.get('cover_i', 0)
    if cover_i:
        meta.cover_url = f"https://covers.openlibrary.org/b/id/{cover_i}-L.jpg"

    logger.debug(f"Found: {meta.title} by {meta.author} ({meta.year})")
    return meta
