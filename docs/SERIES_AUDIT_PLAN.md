# ABS Library Series Audit & Repair Plan

**Author:** audioTransfer planning
**Date:** 2026-08-05
**Scope:** Audit all 935 books / 414 series in the ABS library (`a7ee8f7e-8726-47ad-bc06-d1763d4c6d85`) for correct series membership, sequence numbers, author consistency, and item grouping; fix confirmed mis-assignments. **Planning only — this document does not execute anything on the remote system.**

---

## 0. Verified Environment Facts (this session)

- Host `roadman`, SSH `root@roadman -o BatchMode=yes` works. ABS container `audiobookshelf` healthy, **v2.36.0** (docs/MISTBORN_ORG_PLAN.md says 2.35.1 — stale, update if touched).
- ABS API at `http://localhost:13378` (inside container network). Working endpoints verified previously: `DELETE /api/items/{id}`, `PATCH /api/series/{id}`, `POST /api/libraries/{id}/scan`.
- ABS SQLite DB on host: `/mnt/docker/audiobookshelf/config/absdatabase.sqlite`. `sqlite3` is installed **on the host** (not in the container). Use `sqlite3 <path>` over SSH, not `docker exec`.
- **Schema correction vs. earlier assumptions** (verified via `.schema`):
  - Series membership is a single join table **`bookSeries`** with an inline `sequence` column: `bookSeries(id, sequence VARCHAR, bookId, seriesId, UNIQUE(bookId, seriesId))`.
  - **No** `seriesBooks` or `seriesBookSequences` tables exist in this version. `bookSeries.sequence` is a string (`"1"`, `"01.1"`, `"4"`, or empty). The UNIQUE constraint means a book cannot be double-linked to the *same* series — "duplicate sequence" therefore means two *different* books sharing a sequence.
  - **`libraryItems` has NO `media` JSON column** in v2.36. `extraData` is `{}` for the books checked and `libraryFiles` only carries per-file info (name/size/mtime), **not** metadata.json content. ABS reads `metadata.json` from disk at scan time (highest precedence `absMetadata`); it is NOT persisted in the DB. Any "what does metadata.json say" analysis must read the files on disk (`find /mnt/media/audiobooks -name metadata.json`) or a full rescan.
- Library metadata precedence (`libraries.settings.metadataPrecedence`): `["folderStructure","audioMetatags","nfoFile","txtFiles","opfFile","absMetadata"]`. **Folder structure is lowest, embedded audio tags override it, metadata.json wins.** Series/author attribution is a mix of all three — never assume one source.
- Counts: 935 books, 935 bookSeries links, 414 series, 0 `isMissing`, all 935 items `isFile=0` (folder-based).

### Recon preview — real anomalies already found during planning (read-only)

These concrete findings anchor the heuristics and tiers below:

| Class | Example (real) | Evidence |
|---|---|---|
| Author-name-as-series | `series "Gerri Hill"` → `/audiobooks/Lesbian Romance 27.07.2018/Gerri Hill/At Seventeen` | Folder dump `<top>/<Author>/<Book>` is misread by ABS as author=top-dump, series=Author; dozens of such series (Georgia Beers, Paris Rivera, A.B. Rutledge, …) |
| Absorbed / identical-title items | `series "Justice"` = 6 books all titled `Justice`; `"The Pike"` ×3; `"Romance"` ×12 all titled `Romance`; `"Best Lesbian Erotica"` ×3 | `COUNT(DISTINCT title)=1` within series (12+ series) |
| Duplicate franchise series | 6 separate series rows: `Pern Novels [publication order]` (15), `[chronological order]` (9), `Dragonriders of Pern Series` (8), `Original Dragonriders of Pern` (2), `Harper Hall of Pern` (2), `Dragonriders of Pern (New Series)` (2) | Many books linked to ≥2 of these (books-in-2-series query) |
| Duplicate sequence numbers | `Zodiac Academy` seq `3` ×2; `Classroom of the Elite` seq `1` ×2; `The Scorched Throne` seq `2` ×2; `Dangerous Damsels` seq `4` ×2 | `GROUP BY seriesId, sequence HAVING COUNT(*)>1` |
| Legit part-numbering (not a bug) | `Ascendance of a Bookworm` seq `01.1` and `01.2` | Parts vs. standalone — must not "fix" |
| Wrong-series orphan link | `series "A Song of Ice and Fire"` (1 member) = `The Book of Swords` at `/Robin Hobb/Realm of the Elderlings/02 Short Pieces/05 Her Father's Sword [novelette]` | Anthology short piece mis-tagged into an unrelated franchise series |
| Single-book "series" | `Aneka Jansen` (real series, `/Niall Teasdale/Aneka Jansen/` numbered), `Afterworlds` (self-referential folder), most `Lesbian Romance` author names | Need per-case classification, cannot blanket-delete |
| Good reference | `Mistborn` = 8 books, sequence 1–8, author Brandon Sanderson | Known-good series `e12727f0-f1f4-4e0c-bd17-b27aa707e284` |

---

## 1. Recon / Audit Queries

Run over SSH against the host DB. Set a shell var to shorten commands:

```bash
DB=/mnt/docker/audiobookshelf/config/absdatabase.sqlite
LIB=a7ee8f7e-8726-47ad-bc06-d1763d4c6d85
ssh root@roadman "sqlite3 -readonly $DB \"<QUERY>\""
```

Use `-readonly` for every recon pass so a mistyped query can never write.

### Q0 — Sanity / baseline counts
```sql
SELECT (SELECT COUNT(*) FROM series) AS series_count,
       (SELECT COUNT(*) FROM bookSeries) AS membership_count,
       (SELECT COUNT(*) FROM books) AS books_count,
       (SELECT COUNT(*) FROM libraryItems WHERE libraryId='$LIB' AND mediaType='book') AS items,
       (SELECT COUNT(*) FROM libraryItems WHERE libraryId='$LIB' AND mediaType='book' AND isMissing=1) AS missing;
```

### Q1 — Per-series inventory (name, sequences, authors, member paths)
The single most useful query. Output a TSV consumed by every downstream agent.
```sql
SELECT s.name AS series,
       bs.sequence AS seq,
       b.title AS book,
       (SELECT GROUP_CONCAT(a.name, ' | ') FROM bookAuthors ba JOIN authors a ON a.id=ba.authorId WHERE ba.bookId=b.id) AS authors,
       li.path
FROM series s
JOIN bookSeries bs ON bs.seriesId = s.id
JOIN books b ON b.id = bs.bookId
JOIN libraryItems li ON li.mediaId = b.id AND li.libraryId='$LIB'
WHERE s.libraryId='$LIB'
ORDER BY s.name, CAST(bs.sequence AS REAL), bs.sequence, b.title;
```
Export: `... > /tmp/opencode/series_inventory.tsv`.

### Q2 — Series where a sequence number is duplicated across DIFFERENT books
```sql
SELECT s.name, bs.sequence, COUNT(*) AS n,
       GROUP_CONCAT(b.title, ' | ') AS books
FROM bookSeries bs
JOIN series s ON s.id = bs.seriesId
JOIN books b ON b.id = bs.bookId
WHERE bs.sequence IS NOT NULL AND bs.sequence != ''
GROUP BY bs.seriesId, bs.sequence
HAVING COUNT(*) > 1
ORDER BY s.name;
```
> Inverted/missing sequences: `sequence = ''` or `NULL` (many legit one-off tags, but flag when a series has 2+ numbered members and one empty). "Inverted" (e.g. `Mistborn` book at seq `8` named `01 - …`) is a title-vs-number cross-check — see Q13.

### Q3 — Author inconsistency WITHIN a series
```sql
SELECT s.name, COUNT(DISTINCT author_set) AS n_author_sets,
       GROUP_CONCAT(DISTINCT author_set, ' | ') AS authors
FROM (
  SELECT bs.seriesId, b.id,
         (SELECT GROUP_CONCAT(a.name, ' | ' ORDER BY a.name) FROM bookAuthors ba JOIN authors a ON a.id=ba.authorId WHERE ba.bookId=b.id) AS author_set
  FROM bookSeries bs JOIN books b ON b.id = bs.bookId
) t
JOIN series s ON s.id = t.seriesId
GROUP BY t.seriesId
HAVING COUNT(DISTINCT author_set) > 1
ORDER BY n_author_sets DESC;
```

### Q4 — Books linked to 2+ DIFFERENT series (franchise split / accidental links)
```sql
SELECT b.title, li.path,
       GROUP_CONCAT(s.name, ' | ') AS series_list
FROM bookSeries bs
JOIN books b ON b.id = bs.bookId
JOIN libraryItems li ON li.mediaId = b.id AND li.libraryId='$LIB'
JOIN series s ON s.id = bs.seriesId
GROUP BY bs.bookId
HAVING COUNT(DISTINCT bs.seriesId) > 1;
```
Confirmed hit-rate: every Pern multi-series book shows up here.

### Q5 — Series whose member books ALL share one title (absorbed/merged grouping)
```sql
SELECT s.name, COUNT(*) AS n
FROM bookSeries bs
JOIN series s ON s.id = bs.seriesId
JOIN books b ON b.id = bs.bookId
GROUP BY bs.seriesId
HAVING COUNT(DISTINCT b.title) = 1 AND COUNT(*) > 1
ORDER BY n DESC;
```
Real hits: `Justice`(6), `Romance`(12), `The Pike`(3), `Best Lesbian Erotica`(3) …

### Q6 — Series named exactly like an author folder or author record
```sql
-- series name that matches an author name (case-insensitive)
SELECT s.name, COUNT(*) AS n
FROM series s
JOIN bookSeries bs ON bs.seriesId = s.id
WHERE EXISTS (SELECT 1 FROM authors a WHERE a.name = s.name COLLATE NOCASE)
GROUP BY s.id
ORDER BY n DESC;

-- series whose name equals its own top-of-path parent folder (author-dir-as-series artifact)
SELECT s.name, COUNT(*) AS n
FROM series s
JOIN bookSeries bs ON bs.seriesId = s.id
JOIN libraryItems li ON li.mediaId = bs.bookId AND li.libraryId='$LIB'
WHERE instr(lower(li.path), lower('/' || s.name || '/')) > 0
  AND li.path LIKE '%Lesbian Romance 27.07.2018%'
GROUP BY s.id
ORDER BY n DESC;
```
> `Aneka Jansen` is a false positive here (real numbered series) — never delete on this query alone; feed to classification.

### Q7 — Single-book series (orphan classification candidates)
```sql
SELECT s.name, b.title, li.path,
       (SELECT GROUP_CONCAT(a.name,' | ') FROM bookAuthors ba JOIN authors a ON a.id=ba.authorId WHERE ba.bookId=b.id) AS authors
FROM series s
JOIN bookSeries bs ON bs.seriesId = s.id
JOIN books b ON b.id = bs.bookId
JOIN libraryItems li ON li.mediaId = b.id AND li.libraryId='$LIB'
WHERE s.id IN (SELECT seriesId FROM bookSeries GROUP BY seriesId HAVING COUNT(*) = 1)
  AND s.libraryId='$LIB'
ORDER BY s.name;
```

### Q8 — Folder structure says "series" but no `bookSeries` link exists (unlinked books)
Disk is the source of truth for grouping. First enumerate series dirs on disk, then diff against DB:
```bash
# every /Author/Series/ with >=2 direct book subdirs that each contain audio
ssh root@roadman "find /mnt/media/audiobooks -mindepth 3 -maxdepth 3 -type d -print" > /tmp/opencode/disk_level3.txt
```
Cross-check each `<...>/<Author>/<Series>/` against the DB series list; for each disk series dir, list its book subdirs and compare with the DB members for that series name. A disk book dir with no matching `bookSeries` row is a "missing link" candidate (MEDIUM).
```sql
-- which series rows exist for reference
SELECT id, name FROM series WHERE libraryId='$LIB' ORDER BY name;
```

### Q9 — metadata.json / folder evidence of series membership NOT reflected in DB
`metadata.json` is on disk only. Batch-extract series fields:
```bash
ssh root@roadman "find /mnt/media/audiobooks -name metadata.json -exec sh -c 'echo \"== \$1\"; cat \"\$1\"' _ {} \;" > /tmp/opencode/metadatajson_dump.txt
```
Parse each for `"series"` / `"seriesName"` / `"seriesSequence"` and join by folder path against the DB inventory (Q1). A file that declares a series but whose book has no `bookSeries` row is a missing-link candidate (MEDIUM if folder also shows the series dir, LOW if only the tag says so).

### Q10 — Near-duplicate series names (franchise fragmentation)
```sql
SELECT s1.name, s2.name AS alt_name
FROM series s1 JOIN series s2 ON s1.libraryId = s2.libraryId AND s1.id != s2.id
WHERE s1.name != s2.name
  AND (s1.name LIKE '%' || s2.name || '%' OR s2.name LIKE '%' || s1.name || '%'
       OR lower(s1.name) = lower(s2.name))
ORDER BY s1.name;
```
Hits: the 6 Pern rows, plus any `Foo` / `Foo Series` / `Foo [something order]` pairs.

### Q11 — Books with identical title across the library (candidate duplicates to dedupe before any link fix)
```sql
SELECT b.title, COUNT(*) AS n, GROUP_CONCAT(li.path, ' | ') AS paths
FROM books b JOIN libraryItems li ON li.mediaId = b.id AND li.libraryId='$LIB'
GROUP BY lower(b.title) HAVING COUNT(*) > 1
ORDER BY n DESC;
```

### Q12 — `isMissing` / `isInvalid` residue (should be 0, re-check after any fix)
```sql
SELECT isMissing, isInvalid, COUNT(*) FROM libraryItems
WHERE libraryId='$LIB' AND mediaType='book' GROUP BY isMissing, isInvalid;
```

### Q13 — Sequence-number vs. folder-prefix cross-check (inverted/duplicate prefixes)
Folder prefixes drive ABS sequence under folderStructure. Dump every book's folder name + its `bookSeries.sequence`:
```bash
ssh root@roadman "find /mnt/media/audiobooks -mindepth 2 -maxdepth 4 -type d" > /tmp/opencode/disk_dirs.txt
```
Then for each series, compare the `NN - ` prefixes on disk with the DB `sequence` values. Flag: two folders with the same `NN` prefix (duplicate on disk), or disk `NN` ≠ DB sequence (inverted/wrong link), or `NN - ` prefix present but DB sequence empty.

### Q14 — Author-folder books that were merged into one item (title falls back to series/folder name)
```sql
SELECT li.path, li.title, li.authorNamesFirstLast,
       json_array_length(b.audioFiles) AS n_audio
FROM libraryItems li JOIN books b ON b.id = li.mediaId
WHERE li.libraryId='$LIB' AND mediaType='book'
  AND json_array_length(b.audioFiles) > 1
  AND li.title LIKE '%/%'  -- placeholder: replace with absorbed-title predicate if needed
ORDER BY n_audio DESC;
```

**Deliverable of Phase 1:** `/tmp/opencode/series_inventory.tsv` + one small report file per anomaly class (Q2–Q14), plus `/tmp/opencode/metadatajson_dump.txt`. Nothing written to the remote.

---

## 2. Matching Heuristic ("book X belongs to series Y")

### 2.1 Evidence sources, in precedence order (matches ABS `metadataPrecedence`)

1. **Folder path** — the strongest and organizer-native signal:
   - `<Author>/<Series>/<NN - <Book>>/` → series = `<Series>`, sequence = `NN`. HIGH.
   - `<Author>/<Book>/` → no series. If `<Book>` itself starts with `<Series> NN` (e.g. `Aneka Jansen 01 Steel Beneath the Skin`), it is a *flat* series book: series = leading run of title tokens, sequence = `NN`. HIGH if the folder's leading run matches an existing series name; MEDIUM otherwise.
   - `<Series>/<Book>/` (no author level) → series from level 1.
   - Warning case: `<TopDump>/<Author>/<Book>/` where `<TopDump>` is a bulk folder (e.g. `Lesbian Romance 27.07.2018`) — ABS misreads level 2 as a series. Classifier must detect dump folders (non-author, non-title top-level names) and treat level 2 as the **author**.
2. **metadata.json (disk)** — `"series": ["Name #N"]`, `"seriesName"`, `"seriesSequence"`. Authoritative when present (highest precedence).
3. **Embedded audio tags** — series/album tag, read live (not in DB). Next precedence. Least trustworthy (source rippers tag series=author — the `Gerri Hill` cause).
4. **Title/author heuristics** — canonical mapping tables (see 2.3).

### 2.2 Matching rules (ordered, first match wins)

- **R1 Folder exact:** book path contains an existing series folder name as its immediate parent and series name + author folder match the DB series author → link + sequence from `NN - ` prefix. **HIGH.**
- **R2 Folder fuzzy:** book path parent equals an existing series name but author differs, or series name matches case/prefix-insensitively (`Pern Novels [publication order]` vs disk `Pern`) → **MEDIUM** (franchise consolidation candidate, human confirm).
- **R3 metadata.json declares** `seriesName`/`series` equal to an existing series and sequence present → **HIGH**. Declares a series that doesn't exist yet → **MEDIUM** (propose new series row).
- **R4 Tag-only series** (audio tag, no folder, no metadata.json) matching an existing series → **MEDIUM**. Matching nothing → **LOW** (review; often author-name-as-series artifact).
- **R5 Title map:** title matches a known canonical list (e.g. `The Hero of Ages` → Mistborn Era 1 seq 3). Only maintain a tiny hand-curated list (Mistborn, Pern, Zodiac Academy) for disambiguation; treat as **MEDIUM** unless the folder also agrees.
- **R6 Author-name-as-series:** series name == an author name, books are standalone (`<Author>/<Book>/`, not `<Author>/<Series>/<Book>/`), no numbering → unlink artifact. **HIGH** to *unlink/restructure* only when the folder at level 2 is the author and there is no `NN - ` prefix and no metadata.json series. Otherwise **LOW** (e.g. `Aneka Jansen` real).

### 2.3 Confidence scoring

| Level | Meaning | Auto-fix? |
|---|---|---|
| **HIGH (90–100)** | Folder path matches an existing series dir AND author consistent (R1), or metadata.json matches an existing series with sequence (R3), or clear unlink artifact (R6). | Yes — tier A |
| **MEDIUM (60–89)** | Title/seriesName match, fuzzy folder match, franchise-split candidates, missing-link candidates (R2/R4/R5, Q4/Q8/Q9). | Report only — tier B |
| **LOW (<60)** | Tag-only, ambiguous single-book "series", anthology short-pieces, dump-folder edge cases (R4-nomatch, Q7). | Human review — tier C |

Every decision writes a one-line record: `series | book | path | rule | score | action | evidence`.

---

## 3. Fix Strategy

### 3.0 Non-negotiable precondition

```bash
TS=$(date +%Y%m%d-%H%M%S)
ssh root@roadman "cp /mnt/docker/audiobookshelf/config/absdatabase.sqlite /mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-$TS && ls -la /mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-$TS"
```
Back up before **any** write, and again immediately before any rescan (rescan itself mutates the DB). Keep each numbered run's backup; do not reuse the same `bak` name (the 2026-08-05 Mistborn backup already exists as `absdatabase.sqlite.bak-20260805`).

### 3.1 Fix mechanism preference (folder-first)

ABS derives grouping from disk; DB-only edits are reverted/overwritten by the next scan. **Order of preference:**

1. **Restructure folders on remote + full rescan** (`POST /api/libraries/{id}/scan`). Matches the organizer's `Author/Series/Book` model. Use for: un-absorbing merged items, promoting dump-folder authors (`Lesbian Romance 27.07.2018/<Author>/` → `/audiobooks/<Author>/`), adding `NN - ` prefixes, moving loose audio files into book dirs.
2. **Write `metadata.json` (disk, highest precedence)** for deterministic series/title/sequence where folder structure can't carry it (short stories, novellas, anthologies, non-numbered entries).
3. **ABS API** for small, non-structural fixes: `PATCH /api/series/{id}` (description), unlink via `PATCH /api/books/{id}` body with `series:[]` — verify actual v2.36 payload in a dry run before bulk use. API edits survive a plain rescan less reliably than folder+metadata.json changes; always re-verify post-scan.
4. **Direct DB edit on a stopped container** (last resort): `docker stop audiobookshelf`, edit `bookSeries`/`series` rows with `sqlite3`, start again. Only for surgical link/sequence corrections where a rescan would re-derive the same wrong grouping. Documented rollback = the backup from 3.0. Prefer this over leaving a wrong link, but never as the *first* choice.

### 3.2 Tiered execution

**Tier A — automated (HIGH confidence only).** Each fix is a dry-run, then executed, then re-verified before the next:
- Unlink author-name-as-series artifacts (R6) by **restructuring** the dump subtree: move `<dump>/<Author>/<Book>` → `/audiobooks/<Author>/<Book>` (removes both the fake series and the fake author). If a full restructure is too invasive for one pass, fall back to deleting the `bookSeries` link + removing the fake series row (DB edit, container stopped) and record that a scan may re-add it if tags still say so — flag for metadata.json shielding.
- Absorbed identical-title series (Q5): move each `<Series>/<NN Title>/` book into its own correctly-named book dir (or add per-book metadata.json with real titles) and rescan.
- Missing sequence numbers where disk has `NN - ` prefixes: rely on folderStructure after rescan; verify with Q2 re-run.
- Franchise duplication (Pern): consolidate onto ONE series row per franchise. Decide canonical name (e.g. `Pern`); re-point all `bookSeries.seriesId` (DB edit, stopped container) or unlink the loser series and re-scan. Sequence numbers per canonical reading order (pick publication order; flag if user prefers chronological).

**Tier B — report-only (MEDIUM).** Generate `/tmp/opencode/tier_b_report.md` listing each candidate with its rule/score/evidence. No writes. Surface to user for approval in batches.

**Tier C — human review (LOW).** `/tmp/opencode/tier_c_review.md` — single-book series classification, anthology short pieces, ambiguous author-as-series, tag-only series. Human decides each; no automated action.

### 3.3 Concrete fix recipes (with commands)

**Recipe 1 — un-absorb a merged series item (folders + rescan).**
```bash
# after backing up: move loose audio out of the series folder, give book dirs NN - prefixes
ssh root@roadman 'cd /mnt/media/audiobooks/BRANDON-SANDERSON-MISTBORN-DIR && \
  mkdir -p "01 - The Final Empire" && mv The\ Final\ Empire/* "01 - The Final Empire/" && ...'
TOKEN=$(ssh root@roadman 'docker exec audiobookshelf node -e "..."')   # see 3.4
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://localhost:13378/api/libraries/a7ee8f7e-8726-47ad-bc06-d1763d4c6d85/scan?force=1
```
(Executed already for Mistborn on 2026-08-05 — this is the repeatable template.)

**Recipe 2 — write metadata.json to pin title/series/sequence.**
```bash
cat > /tmp/opencode/meta.json <<'JSON'
{ "title": "The Inheritance", "series": ["Realm of the Elderlings #2.6"], "authors": ["Robin Hobb"] }
JSON
scp /tmp/opencode/meta.json root@roadman:/mnt/media/audiobooks/Robin\ Hobb/Realm\ of\ the\ Elderlings/02\ Short\ Pieces/06\ The\ Inheritance\ \[novelette\]/metadata.json
```
Then rescan. This is the **only** robust way to fix the mis-titled short-pieces and anthology items (they have no folder numbering to lean on).

**Recipe 3 — promote dump-folder authors (large restructure, phased + gated).**
```bash
# dry run first: list everything that would move
ssh root@roadman 'find /mnt/media/audiobooks/Lesbian\ Romance\ 27.07.2018 -mindepth 2 -maxdepth 2 -type d' > /tmp/opencode/dump_level2.txt
# (after human sign-off) mv each <dump>/<Author>/ -> /audiobooks/<Author>/
```
Must be a single-writer job (see §4) because it touches hundreds of dirs.

**Recipe 4 — consolidate duplicate franchise series (DB, container stopped).**
```bash
ssh root@roadman 'docker stop audiobookshelf'
ssh root@roadman "sqlite3 /mnt/docker/audiobookshelf/config/absdatabase.sqlite \
  \"BEGIN; UPDATE bookSeries SET seriesId='<CANONICAL>' WHERE seriesId IN (<LOSERS>); \
   DELETE FROM series WHERE id IN (<LOSERS>); COMMIT;\""
ssh root@roadman 'docker start audiobookshelf'
# verify: Q1 for the franchise, then a normal (non-forced) scan and Q1 again
```
> Sequence conflicts must be resolved before UPDATE (merge by chosen reading order). Never leave two books with the same sequence in the consolidated series — run Q2 after the merge, before committing, to catch it.

### 3.4 API auth (verified recipe)
```bash
TOKEN=$(ssh root@roadman 'docker exec audiobookshelf node -e '\''const jwt=require("/usr/local/lib/node_modules/jsonwebtoken");const {v4:uuid}=require("uuid");const db=require("better-sqlite3")("/config/absdatabase.sqlite");const row=db.prepare("SELECT value FROM settings WHERE key=\"server-settings\"").get();const secret=JSON.parse(row.value).tokenSecret;console.log(jwt.sign({userId:"d055d68a-317b-408e-87f3-8a3f6c23fd57",username:"Jevonx",jti:uuid(),type:"access"},secret,{expiresIn:3600}))'\''')
```
Endpoints used (all via `http://localhost:13378` from the container network / port forward):
| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/libraries/{libId}/scan` | rescan (`?force=1` for structural changes) |
| `DELETE` | `/api/items/{itemId}` | remove a stale/absorbed item before rescan (⚠️ drops listening progress) |
| `PATCH` | `/api/series/{seriesId}` | description (safe) |
| `PATCH` | `/api/books/{bookId}` | series/sequence adjustments — **verify v2.36 payload on one test book first** |

### 3.5 Rollback
- DB: restore `absdatabase.sqlite.bak-<TS>` (stop container, `cp` back, start). Only needed for DB-write recipes; folder moves + metadata.json are re-scan-undoable but leave a trail — keep a `mv`-log (`.bak` suffix renames) for the restructure recipes.
- Folders: every `mv` is paired with a log line (`path_old\tpath_new`) written to `/tmp/opencode/restructure.log` so a human can reverse.
- Listens/progress: `DELETE /api/items/{id}` removes progress for that item — document in the run log; user approved this pattern on 2026-08-05.

---

## 4. Multi-Agent Orchestration Plan

Goal: parallelize without two agents touching the same path or the same DB row set.

### 4.1 Ownership model — split by disjoint top-level author subtree

- The library root is `/mnt/media/audiobooks/`. **Partition the library by first-level directory.** Each audit/fix agent owns one or more complete first-level subtrees (`Lesbian Romance 27.07.2018/`, `Brandon Sanderson/`, `Robin Hobb/`, …) plus the series/`bookSeries` rows that reference only books under those subtrees.
- **DB-row ownership is derived from folder ownership:** an agent may only modify `bookSeries`/`series` rows whose member books' paths fall inside its subtree. The ONE exception — franchise-consolidation (Pern) — is assigned to a single dedicated agent because its books span multiple author subtrees.
- No two agents ever share a path prefix. Enforced by a partition table (below) written by the investigator and read-only for everyone else.

| Agent | Role | Owns | Writes |
|---|---|---|---|
| **investigator** | Phase-1 recon (all of §1), writes inventory + reports | read-only everywhere | `/tmp/opencode/*` only |
| **audit-N** (per subtree bucket, e.g. 4 agents) | Re-run Q1 filtered to bucket; classify memberships (HIGH/MEDIUM/LOW); produce per-bucket fix list | one disjoint subtree set | `/tmp/opencode/bucket-N.*` |
| **fix-N** (after human gate) | Apply tier-A fixes for one bucket (folders + metadata.json); log every action | same disjoint subtree as audit-N | only its subtree + `restructure.log` |
| **peln-franchise** | Pern consolidation (spans buckets) | the 6 Pern series rows + their books only | those rows only |
| **verifier** | Phase-5 validation; re-runs Q0–Q3, Q5, Q12 globally; audits restructure.log | read-only | `/tmp/opencode/verify_report.md` |
| **orchestrator (me)** | partition, gates, merge, human escalations | — | coordination only |

### 4.2 Shared context handoff

- All intermediates in **`/tmp/opencode/`** (local) or **`/tmp/opencode/`** on roadman for raw query output. Namespaced files, one writer per file:
  - `series_inventory.tsv` (investigator)
  - `tier_A_fixes.<bucket>.md`, `tier_B_report.md`, `tier_C_review.md`
  - `restructure.log` (append-only, all fix agents append; row-level lock via a single append script to avoid torn lines)
  - `verify_report.md` (verifier)
- Folder partition table: `/tmp/opencode/partition.tsv` — `owner<TAB>top_level_path_prefix`. Fixed before any agent starts.
- Concurrency bounds: **≤4 audit agents + ≤4 fix agents in parallel**, never more than one agent with write access to the remote filesystem at a time per subtree; the DB (when used) is edited by **one** agent at a time under an exclusive SSH lock (`flock` a file on roadman or a `docker stop` window). SSH to roadman is single-writer for filesystem mutations because concurrent `mv`/`scp` on the same host has previously caused failures (TRANSFER_NOTES.md) — serialize writes, parallelize reads.
- Merge/verify gate: **no fix is merged until (a) its tier-A list was approved, (b) the verifier's global re-run is clean for the affected series, (c) `restructure.log` shows 1:1 old→new with no collisions, (d) a non-forced rescan did not re-introduce the anomaly.**

### 4.3 Phase gates

1. **G0** investigator recon → reports to `/tmp/opencode/`. (No writes to remote.)
2. **G1 (human)** — approve tier-A fixes per bucket. Tier B/C published as reports, not acted on.
3. **G2** fix agents execute tier-A + apply metadata.json shielding, logging every action; franchise agent runs its consolidation.
4. **G3 (verifier)** — full validation (§5). If any check fails, the responsible bucket is handed back with the failing query output; the fix agent repairs and returns to G3.
5. **G4 (done)** — exit criteria met (§5); write run notes to `docs/`.

---

## 5. Validation & Exit Criteria

After fixes and a rescan, the verifier re-runs and asserts:

1. **Q1 inventory re-run** — for every previously-fixed series: correct member set, sequences 1..N contiguous (allow `01.1`/`01.2` part numbers), author set consistent.
2. **Q2 re-run → empty** for every consolidated/renumbered series (no duplicate sequence across different books). Intentional exceptions (part-numbered `Ascendance of a Bookworm` style) recorded explicitly in the verify report.
3. **Q4 re-run → empty or fully explained** (no book in 2+ series unless franchise design intends it — after Pern consolidation, none).
4. **Q5 re-run → empty** (no series whose members all share one title).
5. **Q12 → `isMissing=0, isInvalid=0`.**
6. **Scan success:** `POST /api/libraries/{id}/scan` returns 200 and the item count stays 935 (no accidental duplicates/merges) and the rescan did **not** resurrect any tier-A fix (compare Q1 before/after a *second*, non-forced scan).
7. **Spot-check via ABS UI:** for ≥3 fixed series (incl. a dump-folder author, a Pern book, an absorbed-title series) confirm correct series page, sequence ordering, author, and book covers; confirm single-book pages show no stray series chip.
8. **restructure.log integrity:** every new path exists, every old path moved, no two rows map to the same target, no file left behind in a series/author folder that still causes absorption (no loose audio above book level).
9. **Exit criteria checklist** (all must hold):
   - [ ] every series' members verified against its disk folder (no unlinked, no mislinked)
   - [ ] no duplicate sequence within any series
   - [ ] no book in two series (except documented franchise intent)
   - [ ] no author-name-as-series artifacts remaining
   - [ ] no absorbed/identical-title series remaining
   - [ ] no orphan single-book "series" left unclassified (each is either a real series, deliberately standalone, or removed)
   - [ ] 935 items, 0 missing/invalid after rescan
   - [ ] backup files present, run log written to `docs/`

---

## 6. Risks & Mitigations

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| 1 | **Same-named series, different authors** (e.g. `Afterworlds` self-referential, `Aneka Jansen` character-named) — blanket author-name-as-series deletion destroys real series | Data loss / wrong unlink | Author-name-as-series is only auto-fixed (HIGH) when folder shows `<dump>/<Author>/<Book>` with no `NN - ` prefix and no metadata.json series; every other case → tier C human review |
| 2 | **Single-book "series" that are anthologies / compilations** (e.g. `Best Lesbian Erotica`, `The Book of Swords` mis-tagged ASOIAF) | Wrong link persists or correct link removed | Tier C; classify per book; prefer metadata.json `series:[]` + title fix over link deletion |
| 3 | **Rescan side effects** — a `?force=1` scan re-groups items, drops manually-set metadata, re-creates links from tags, or re-merges if a loose file remains | Regression across whole library | Non-forced scan after each fix batch; compare Q1 diff; never force-scan a large batch without backup; keep `libraries.settings.metadataPrecedence` unchanged so metadata.json stays authoritative |
| 4 | **DB vs API vs disk drift** — DB edit reverted by next scan, or API patch overwritten | Fix looks done then silently undone | Folder + metadata.json changes are durable (re-scan-consistent); DB/API edits are always followed by a verification scan and marked "scan-fragile" in the log; verify ≥2 scans later |
| 5 | **Sequence semantics** — `01.1`/`01.2` parts, `1` vs `01`, `"Romance"` in a sequence field, empty sequences | False "duplicate" alarms or wrong renumber | Only renumber after Q2 triage; treat part-numbering and empty-as-standalone as legitimate unless folder contradicts; sequence is VARCHAR — normalize with `CAST(... AS REAL)` for ordering only |
| 6 | **Dump-folder restructure volume** (`Lesbian Romance 27.07.2018`, ~382 subdirs) — massive `mv`, risk of interrupted move, SSH transfer flakiness | Partial/failed move, broken library | Phased (one letter-range at a time), dry-run lists first, `mv` log with `.bak` fallback, serialize all writes to roadman (TRANSFER_NOTES.md: concurrent SSH ops fail), rescan after each phase |
| 7 | **Loose audio above book level re-merging books** (the 2026-08-05 Mistborn cause) | New merged items | Pre-fix audit adds `find <series-dir> -maxdepth 1 -type f` check; any audio at author/series level is moved into a book dir first |
| 8 | **Progress/listen loss** on `DELETE /api/items/{id}` for absorbed items | User frustration | Only delete when a correct replacement item exists; document every deleted itemId + title in the run log; user pre-approves (as on 2026-08-05) |
| 9 | **Parallel agent writes colliding** (two agents moving files under the same author, or DB row overlap) | Corruption | Disjoint subtree partition (§4.1); single-writer DB window via `flock`/`docker stop`; verifier checks `restructure.log` for 1:1 mapping before merge |
| 10 | **Book identity drift** (title/author disagreement between folder, tags, metadata.json) affecting series grouping | Wrong membership after fix | metadata.json written to *every* touched book so `absMetadata` (highest precedence) pins title/author/series/sequence deterministically |
| 11 | **Franchise reading-order disagreement** (Pern publication vs chronological) | Wrong sequence after consolidation | Default publication order; record the choice in the run log and expose as a decision point at G1 |
| 12 | **Backup rotation / stale `bak` reuse** | Can't roll back | Unique timestamped backup per write session; never overwrite an existing `.bak`; verify backup file size after `cp` |

---

## 7. Deliverables / Run Notes

- Executed-fix log appended to `docs/TRANSFER_NOTES.md` (as with prior roadman fixes) and this plan updated with `**Status:** ✅ EXECUTED <date>` when complete.
- Reports kept at `/tmp/opencode/` (ephemeral) with a summary copy committed to `docs/` if the user wants an audit trail.
- Exit criteria from §5 are the definition of done; nothing is marked done until every box is checked and the verifier's report is clean.

---
## Execution Status (2026-08-05)

**COMPLETED.** Full audit executed and fixes applied. See `docs/SERIES_AUDIT_RESULTS.md`.
- 209 bogus author-as-series rows + 350 links purged (Lesbian Romance dump artifacts)
- 86 orphaned bookSeries links cleaned
- Shield Hero, Narnia, Elderlings, Elements of Cadence, COTE franchises consolidated
- Farseer/Elderlings + Zodiac part-numbering left intact (legitimate)
- Post-scan validation: 185 series / 486 links / 0 orphans / 0 isMissing / 0 series==author
- Backup: `/mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-seri-fix-20260805`
