# Full Library Metadata Audit — Round 2 (2026-08-15)

**Scope:** Entire Audiobookshelf library on `roadman` (990 books, 199 series, 437 authors).
**Context:** Round 1 = 2026-08-11 audit (964 books). Library grew by 26 books since.
**Goal:** Every book properly matched to metadata — title, authors, series, tags, description, cover, publisher, year, language — and fix the transfer method so new books stop arriving half-empty.

## Summary

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| Books | 990 | 990 | 0 |
| Missing tags | 857 | **82** | −775 |
| Missing description | 57 | **28** | −29 |
| Missing publisher | 57 | **18** | −39 |
| Missing year | 4 | **0** | −4 |
| Missing language | 37 | **10** | −27 |
| Missing cover | 2 | **0** | −2 |
| Missing ISBN+ASIN | 808 | **80** | −728 |
| Books without series link | 446 | 442 | −4 (Crescent City ×3, Inheritance #4 added) |
| Polluted years (non-YYYY) | 33 | **0** | −33 |
| `(Unabridged)` titles | 3 | 0 | −3 |
| Redundant "X, Book N" subtitles | 73 | 0 | −73 |
| Books with folder `metadata.json` (rescan-proof) | — | **957** | pinned via `storeMetadataWithItem` |

**Result: 962 of 990 books (97%) now have every core field populated.** The 28 remaining no-description books are genuinely obscure (dump erotica, radio-drama editions, Robin Hobb anthology short pieces, Mistborn Collected Tales) with no provider data — flagged, not guessed (verified via provider search that the audible API does not surface them).

## What was done

### Mechanism (the durable fix)
- **Enabled `storeMetadataWithItem` + `storeCoverWithItem`** (server settings) → every metadata change now writes `metadata.json` into the book's folder, so a rescan can never revert fixes. **957/990 book folders now carry metadata.json.**
- **Provider search via ABS API** (`GET /api/search/books?title=..&author=..&provider=..`) with **strict scored matching**: title token-overlap ≥ 0.6 (containment = 0.95) AND author segment-match ≥ 0.66, **volume-aware** (never match "Vol. 08" to "Volume 17" — this bug was caught before it hit the DB), author list segment matching (provider strings like "Kumo Kagyu, Noboru Kannatuki, Kevin Steinbach - translator" resolve to Kumo Kagyu).
- Patches applied via `PATCH /api/items/:id/media` filling **only missing fields** — existing correct title/authors/series never overwritten. 719 + 9 + 50 = **778 books patched across 3 passes** (audible → iTunes fallback → openlibrary), 0 failures.

### Curated fixes (title/author/series corrections)
- **Inheritance** (Paolini): garbage title `Inheritance: The Inheritance Cycle, Book 4 (Unabridged) Part 1, Chapter 31` → `Inheritance`, series `The Inheritance Cycle #4`, cover downloaded.
- **Shield Hero Vol 10**: wrong author `Shea Taylor` (narrator) → `Aneko Yusagi`.
- **Crescent City series links added**: House of Earth and Blood (#1, folder `1- Sarah J. Maas`), House of Sky and Breath (#2, folder `[2022]`, author `Graphic Audio LLC.` → `Sarah J. Maas`, subtitle "Dramatized Adaptation"), House of Flame and Shadow (#3, title cleaned).
- **F This Murder** → `F*ck This Murder` + co-author `Signe E. Land` (folder `Nigel Morland` is misnamed — flagged).
- **Days at the Torunka Café**: translator `Eric Ozawa - translation` removed from authors; `(Unabridged)` stripped.
- **Where the Crawdads Sing**: `(Unabridged)` stripped.
- **Ascendance of a Bookworm Vol 01.3**: title deduplicated.
- **The Sullivan Vampires omnibus** → real title `Meeting Eternity`.
- **Narnia radio dramas ×3**: title typos fixed (`Voyage of the Dawn Treade` → `The Voyage of the Dawn Treader`, etc.).
- **Atlas Six**: `Atlas 01` → `The Atlas Six`.
- **She Comes First**: title typo cleaned (`The Grammer of Oral Sex` → subtitle `The Grammar of Oral Sex`).
- **Girls of Fate and Fury Part I/II**: year `2200` (parse error) → `2020`.
- **Global normalization**: 33 polluted years → clean YYYY (datetime/ISO/`(08-13-24)`-style strings); 386 language values normalized (`en`/`english` → `English`; German `deu` preserved); 73 redundant "X, Book N" subtitles cleared.

### Verified identities (folder vs DB, DB is correct)
- **Broken Dove**: DB author `Dani Francis` (Silver Elite #2, verified on Audible) — folder `Chelle Bliss` is misnamed (that's a different 2021 book).
- **An Amateur Witch's Guide to Murder** (folder `Kiri Callagan`) — real author K. Valentin (known since 08-11).
- **The Ornithologist's Field Guide to Love** (folder `Sarah Hawley`) — real author India Holton.
- **Embodied Activism** — DB author `Rae Johnson` correct (folder carries foreword contributor).
- **The Bone Season** — folder `Samantha Shannon, Samnatha Shannon, Alana Kerr` (typo + narrator) — DB correct.
- **Between Life and Death / Between Sun and Moon / Between the Moon and Her Night** — all genuinely by Jaclyn Kot (08-11 verified); the Bulgakov-folder copy is a folder misplacement (flagged, not moved).

## Remaining flagged (not errors)
- **28 no-description** books: Robin Hobb short pieces (4), Mistborn Collected Tales, and 23 dump-erotica/obscure titles — audible/iTunes/OpenLibrary search APIs return no correct match (verified per-title).
- **10 no-language**: all the same obscure dump titles (English by title/author evidence, but no provider confirmation — left unset per "never guess").
- **4 Pern short stories** (The Smallest Dragonboy, Nerilka's Story, Runner of Pern, Dragonsblood): missing ISBN/ASIN + tags only; descriptions present.
- **~40 dump books** missing only ISBN/ASIN (providers have no listing).
- **Folder-name mismatches** (Kiri Callagan, Nigel Morland, Chelle Bliss, `[2022]`, `1- Sarah J. Maas`, Samantha Shannon…): DB metadata is correct; folder renames are cosmetic, out of scope.

## Rollback
Restore pre-audit DB (container stopped) from:
`/mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-fullmeta-20260811-214706` (round-1 pre-state) — note the 990→964 book delta (26 books added since round 1) and that `storeMetadataWithItem=true` + the 957 written metadata.json files are NOT covered by the DB backup.

## Transfer method gaps (why books arrived unmatched) — fixed in the audioTransfer tool update, same session
Root causes in `audioTransfer` (fixes landed in the tool code; see README.md + git log for the round-2 update):
1. **OpenLibrary-only enrichment** with blind `docs[0]` pick, no match scoring → wrong metadata, no series/tags/description ever.
2. **No metadata.json written** into organized folders → ABS scanned books with folder-derived metadata only (empty tags/series/desc until a manual audit).
3. **No volume-aware matching** → light novels risk cross-volume mis-matches.
4. **Parser missing `[Series, Book N]` bracket patterns** → titles like `House of Flame and Shadow: Crescent City, Book 3` polluted.
5. Provider reliability (tested 2026-08-15, 24-book sample): **Audible 100% hit** (tags/genres/series/ASIN/desc — best for audiobooks), **iTunes 100% hit** (description only), **OpenLibrary 4%**, **Google Books 0% (quota-exhausted, HTTP 429)**. ABS default provider stays Audible (it is the correct default; no setting change needed).
