# Series Assignment Audit — Results

**Date:** 2026-08-05
**Scope:** Entire Audiobookshelf library on `roadman` (935 books, 407 series pre-audit).
**Goal:** Ensure every book that belongs to a series is properly assigned (correct series, sequence, author), and remove false/duplicate series memberships.
**Plan:** `docs/SERIES_AUDIT_PLAN.md`
**Backup:** `/mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-seri-fix-20260805`

## Summary

| Metric | Before | After |
|--------|--------|-------|
| Series rows | 414 | 185 |
| `bookSeries` links | 935 | 486 |
| Series named after an author (bogus) | 209 | 0 |
| Orphaned bookSeries links | 86 | 0 |
| Books linked to 2+ series | 19 | 1 |
| Duplicate sequence pairs | 2 | 2 (both legitimate) |
| `isMissing` items | 0 | 0 |

## What was done

### 1. Purged 209 bogus "author-as-series" rows (350 links)
The `Lesbian Romance 27.07.2018/` dump folder structure is `<Author>/<Book>/` (no series subdir) and every book's `metadata.json` declares `"series": []`. ABS had nevertheless created 209 series rows named after the author (e.g. `A.J. Quinn`, `Gerri Hill`, `Paris Rivera`) and linked 350 books to them.

- Verified: all 350 links point at books inside the Lesbian Romance dump; none of those books link to any real series; every metadata.json has empty `series`.
- **Fix:** deleted the 350 `bookSeries` links and the 209 empty `series` rows. 406 `metadata.json` files were normalized to empty `series` so the scan cannot re-create them.

### 2. Cleaned 86 orphaned `bookSeries` links
Rows whose `bookId` no longer exists in `books` (leftovers from earlier item merges/deletes). Deleted; also removed the now-empty orphan series rows they referenced (e.g. stale `Classroom of the Elite Year 1` / `Year 2` rows whose 25 links were all orphans).

### 3. Consolidated franchise series
- **Shield Hero** (`Aneko Yusagi`): 3 duplicate series rows (`Rising of the Shield Hero`, `Rising of the Shield Hero Series`, `The Rising of the Shield Hero`) → **1 series `The Rising of the Shield Hero`** with all 12 books and correct sequences 1–12. `metadata.json` written for all 12 book dirs (`series: ["The Rising of the Shield Hero #N"]`).
- **Narnia** (`C.S. Lewis`): 2 series rows (`Publication Order` + `Author's Preferred Order`, each holding the same 1 book) → **1 series `The Chronicles of Narnia`** with all 6 books (Lion=2, Horse=3, Caspian=4, Voyage=5, Silver Chair=6, Last Battle=7). `metadata.json` written for all 6.
- **Pern** (`Anne McCaffrey`): already consolidated by the prior session's partial run to `Pern Novels [publication order]` (23 books); verified intact.
- **Realm of the Elderlings** (`Robin Hobb`): `Realms of the Elderlings` (typo duplicate) merged into `Realm of the Elderlings`; a novelette wrongly tagged "A Song of Ice and Fire" unlinked (that series row deleted).
- **Elements of Cadence** (`Rebecca Ross`): `Elements of Cadence series` merged into `Elements of Cadence` (2 books).
- **COTE** (`Syougo Kinugasa`): stale orphan series rows removed; kept `Classroom of the Elite` (9, Year 1) and `Classroom of the Elite: Year 2` (10) — clean two-series split matching the folder layout.

### 4. Love's Academic / Dangerous Damsels
`The Ornithologist's Field Guide to Love` (`Sarah Hawley`) was wrongly linked to `Dangerous Damsels` (different series by a different author). Removed that link; it now lives only in `Love's Academic` (sequence 2), matching its folder.

## Remaining legitimate notes (no action taken)

1. **Farseer sub-series:** `The Farseer: Assassin's Apprentice` is in both `Realm of the Elderlings` (meta-series) and `Farseer Trilogy` (sub-series). This is correct ABS behavior — intentional.
2. **Zodiac Academy sequence 3 duplicated:** `The Reckoning (Part 1 of 2)` and `(Part 2 of 2)` both have sequence 3. Legitimate part-numbering, not an error.
3. **Shield Hero sequence 4 duplicated:** two distinct Vol 4 rips exist (`199,582,932` vs `199,921,620` bytes — different source rips, same content). Kept both, flagged for human review.
4. **Single-book series** (e.g. `Afterworlds`, `Hatchet`, `The Last Unicorn`): these are metadata-driven standalone titles with a `series` tag set; not ABS misreads, left untouched.
5. A handful of series still mirror author names (e.g. `Elora Bishop & Bridget Essex`, `Cynthia Dane & Hildred Billings`). Unlike the purged 209, these have metadata-driven series tags and co-author pairings; low priority, left for review.

## Rollback
```bash
# Restore the pre-audit DB (container stopped):
docker stop audiobookshelf
cp /mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-seri-fix-20260805 \
   /mnt/docker/audiobookshelf/config/absdatabase.sqlite
docker start audiobookshelf
```
Note: `metadata.json` files were also rewritten (406 normalized to empty series; 12 Shield Hero + 6 Narnia + 1 Love's Academic series tags). Those on-disk changes are not covered by the DB backup and would need `git`/file-level restore if a full rollback is required.
