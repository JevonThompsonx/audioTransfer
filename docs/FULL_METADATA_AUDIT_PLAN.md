# Full Library Metadata Audit & Repair Plan

**Author:** audioTransfer orchestrator
**Date:** 2026-08-11
**Scope:** All 965 books / 194 series / 459 authors in the Audiobookshelf library on `roadman` (Tailscale `100.116.138.103`, DB `/mnt/docker/audiobookshelf/config/absdatabase.sqlite`, API `http://localhost:13379`, library `a7ee8f7e-8726-47ad-bc06-d1763d4c6d85`).
**Goal:** Every book has correct title, authors, series (registered + numbered), and useful day-to-day metadata (description, cover, narrator, publisher, year, language, genre).
**Backup:** `/mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-fullmeta-20260811-214706`

---

## Issue Catalog (from recon, 2026-08-11)

| # | Class | Count | Detail |
|---|-------|-------|--------|
| A | Title == series name only | 296 | DB `title` = series name (`"Romance"`); real title in folder (`Romance 01 Love's Melody Lost`). 282 extractable via folder regex; 14 special (anthologies/omnibus). Root cause: corrupt `metadata.json` (highest precedence) written with title=series. |
| B | Title pollution | ~50 | `(Unabridged)` suffix (36), `[brackets]` (7), series-prefix (`Elite Operatives Book 1`, `Haunting Danielle Bk 16`), embedded-newline titles, "Book 3" style suffixes. |
| C | Narrator/translator/illustrator as author | ~20 | `Kurt Kanazawa`, `Matthew Bridges`, `Romy Nordlinger`, `James Konicek`, `Kevin Gifford - translator`, `Mitz Mitz Vah`, `Quof - Translator`, `Bayo Akomolafe - foreword`, etc. |
| D | Wrong / split authors | ~8 | `Kurt \| Vonnegut` (split), `Jenny Lawson/Jenny Lawson` (dup), `Jaclyn Kot` for Dolores Cannon (narrator), `Cameron Sullivan` for Henry Neff (narrator), `K. Valentin` for Kiri Callagan (narrator), `Kurt Vonnegut Jr` vs `Kurt Vonnegut`, `don Miguel Ruiz` case. |
| E | Missing/blank series sequence | 26 | Haunting Danielle 18 blank, Various 4, Last Unicorn 1, Embodied Activism 1, Rick Riordan Presents 1, Foundryside Founders 1. |
| F | Cross-author series collisions | 2+ | `Nemesis` = April Daniels (Dreadnought) + K.A. Kron (Injustice) — two different series! `Under Her Spell` duplicated across two author dirs. `Various` mixed anthology (acceptable). |
| G | Missing descriptions | 731 | Enrich via provider match (audible/google/openlibrary) or folder/known data. |
| H | Missing covers | 6 | Fetch via cover provider / embed from source. |
| I | Missing publisher / year / language | 804 / 405 / 443 | Enrich via provider match. |
| J | Missing series for books that belong to one | ~40 | Multi-book authors (Paris Rivera 11, Gerri Hill 10, Georgia Beers 10, …) whose titles are standalone but series not registered — verify each, register real series only. |
| K | Local leftovers (aphrodite) | 66 files | qbit files already transferred (Gilded Wolves, Red Winter, Elements of Cadence, iron-widow.mp4) — cleanup candidates on source machine; verify remote parity first. |

## Strategy

1. **Never touch audio content.** Only DB + `metadata.json` (ABS reads with highest precedence). Audio tags already contain correct titles/descriptions for inAudible rips.
2. **Backup before every write phase** (`cp absdatabase.sqlite ...bak-phaseN`).
3. **Prefer ABS API** (`PATCH /api/items/:id/media`, `POST /api/items/:id/match`) so DB cache + sockets stay consistent; fall back to direct SQLite for bulk join-table ops only when API lacks the shape.
4. **metadata.json rewrite** for books whose on-disk metadata is corrupt, so a future rescan cannot revert fixes.
5. **Verify after every phase** — counts, spot-check via API `GET /api/items/:id`, and a final full re-audit.

## Phases

### Phase 1 — Backup + catalog export (DONE)
- Backed up DB, exported `catalog.tsv` (965 books: li id, book id, path, title, subtitle, authors, series, year, lang, publisher).
- Exported `series_list.tsv` (194 series + member counts + sequences).

### Phase 2 — Title fixes (A + B)
- For each of 296 title==series books: extract real title from folder (`<Series> <seq> <Title>` → title=`<Title>`), keep series + sequence.
- Strip `(Unabridged)`, `[x]`, `Book N` suffixes from titles; fix embedded-newline title.
- Apply via API `PATCH /items/:id/media` with corrected title/subtitle; write corrected `metadata.json`.

### Phase 3 — Author fixes (C + D)
- Remove narrator/translator/illustrator entries from author lists; merge duplicate/split author names (`Kurt|Vonnegut` → `Kurt Vonnegut`).
- Re-run quickmatch for books with narrator-as-author (e.g. Shield Hero, Fuse, Kumo Kagyu) to pull real author from provider, then prune narrators.
- Apply via API; write `metadata.json`.

### Phase 4 — Series registration & sequences (E + F + J)
- Fill blank sequences for Haunting Danielle (18), assign via publication order / provider data.
- Split cross-author series (`Nemesis`), de-dup `Under Her Spell`.
- Register real series for multi-book authors where books genuinely belong to one (verify each via provider).
- Apply via API; write `metadata.json`.

### Phase 5 — Enrichment (G + H + I)
- Run `POST /api/items/:id/match` (audible → google → openlibrary) for the 731 books missing descriptions; accept only matches with description.
- Fetch covers for the 6 books missing them.
- Fill publisher/year/language from provider matches where missing.

### Phase 6 — Verify (all)
- Full re-audit: rerun all recon queries, compare before/after counts.
- Spot-check 20+ books via API and audio playback metadata.
- Run `verifier`-style independent pass over the diff.

### Phase 7 — Report
- Write `docs/FULL_METADATA_AUDIT_RESULTS.md` with before/after table, all changes, rollback instructions.

## Rollback
```bash
docker stop audiobookshelf
cp /mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-fullmeta-20260811-214706 \
   /mnt/docker/audiobookshelf/config/absdatabase.sqlite
docker start audiobookshelf
```
Note: rewritten `metadata.json` files are not covered by the DB backup.
