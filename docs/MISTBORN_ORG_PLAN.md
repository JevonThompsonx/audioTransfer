# Mistborn Series Reorganization Plan

**Status:** ✅ **EXECUTED 2026-08-05** — all phases complete, verified in ABS DB.

### Execution Results
- All 8 Mistborn books created as separate items in the `Mistborn` series, sequence 1-8, author **Brandon Sanderson**.
- File counts verified: Final Empire 19, Well of Ascension 3, Hero of Ages 3, Alloy of Law 7, Shadows of Self 8, Bands of Mourning 10, Lost Metal 1, Secret History collection 1.
- Track order fixed for **Bands of Mourning** (was interleaving two discs 1-5; re-tagged 1-10 sequential via ffmpeg) and **Hero of Ages** (re-tagged 1-3).
- Cover fetched from OpenLibrary for the Secret History collection (id 8737946).
- Stray images cleaned (Lost Metal screenshots, Hero redundant covers).
- Merged ABS item `ece022ba` deleted; forced library scan re-added everything.
- ABS DB backed up at `absdatabase.sqlite.bak-20260805` before changes.
**Author:** audioTransfer planning
**Date:** 2026-08-05
**Scope:** Fix Mistborn appearing as one merged book in Audiobookshelf; make it a properly-ordered series with correct metadata and files.

---

## 1. Problem Statement

In Audiobookshelf, the Mistborn series currently appears as **ONE book** (title "Mistborn: Secret History, The Eleventh Metal, and Allomancer Jak and the Pits of Eltania", author "GraphicAudio", duration ≈117h) containing **all 52 audio files** across 8 releases.

### Root Cause (confirmed from ABS internals)

Audiobookshelf's scanner groups media files into "library items" by finding the **deepest directory that directly contains an audio file** and anchoring the group there (`groupFileItemsIntoLibraryItemDirs`, `server/utils/scandir.js`). Because:

- `/mnt/media/audiobooks/Brandon Sanderson/Mistborn/Mistborn- Collected Tales.m4a` is a **loose audio file sitting directly in the series folder** `Mistborn/`, AND
- that same `Mistborn/` folder also contains 7 book subfolders,

ABS treats the *series folder itself* as one giant book and recursively absorbs every audio file from every subfolder. The `Mistborn- Collected Tales.m4a` file's embedded tags (`tagAlbum = "Mistborn: Secret History, The Eleventh Metal, and Allomancer Jak and the Pits of Eltania"`, `tagArtist = GraphicAudio`) supplied the merged item's title/author.

This is a documented ABS pitfall: any loose audio file at the author- or series-folder level absorbs all nested audio into one item. (Docs: "Book Grouping" under directory-structure.)

---

## 2. Current State (verified 2026-08-05)

All under `/mnt/media/audiobooks/Brandon Sanderson/Mistborn/` on `roadman`:

| Book folder | # audio files | Files | Track tags | ABS-visible issues |
|---|---|---|---|---|
| (loose) `Mistborn- Collected Tales.m4a` | 1 | `Mistborn- Collected Tales.m4a` | album="Mistborn: Secret History, The Eleventh Metal, and Allomancer Jak and the Pits of Eltania", artist=GraphicAudio | **CAUSES THE MERGE** |
| `The Final Empire/` | 19 | MISTBORN0101P01–0103P07 | **no embedded tags** (only LAME encoder) | title/order rely on filename |
| `Well of Ascension/` | 3 | pt 1–3 | album="Brandon Sanderson - Mistborn 02 - The Well of Ascension pt N", track 1–3 | album text is verbose/wrong-ish |
| `Hero of Ages/` | 3 | MISTBORN0301–0303 | album="Mistborn 3 - The Hero of Ages (N of 3)", all track=1 | **all 3 files have track=1** → ordering hazard |
| `Alloy of Law/` | 7 | MISTBORN04P01–P07 | album="Mistborn 4: The Alloy of Law", track 1–7 | good |
| `Shadows of Self/` | 8 | MISTBORN05P01–P08 | album="Mistborn 5: Shadows of Self", track 01/08–08/08 | good |
| `Bands of Mourning/` | 10 | MISTBORN0601P01–0602P05 | album="Mistborn 6: The Bands of Mourning (N of 2)", track 1–5 ×2 | two discs both track 1–5 → ordering hazard |
| `The Lost Metal/` | 1 | `Brandon Sanderson - The Lost Metal꞉ A Mistborn Novel.m4b` | album="The Lost Metal (Unabridged)" | ok |

Total audio: 52 files, ~26.3 GB (12,502,476,722 bytes) on remote.

**Cover images present:** every book folder has a `cover.jpg` (Alloy of Law also has ElendelBasin/CityofElendel; Hero of Ages has 5 covers; Lost Metal has 4 images including 2 PNG screenshots). The loose Collected Tales m4a has **no cover file**.

**ABS DB state:** one `libraryItems` row `ece022ba-d5ec-4461-a4a0-73cf4197052f` (path `/audiobooks/Brandon Sanderson/Mistborn`, `isFile=0`), one `books` row, author linked to "GraphicAudio", **no series row**, `bookSeries` empty for it. `libraries.lastScan` = 2026-06-30 (before re-org), ABS v2.35.1.

---

## 3. Target Structure

Audiobookshelf requires **`/Author/Series/Book/files`** with **no loose audio files in the series or author folders**. Target:

```
/mnt/media/audiobooks/Brandon Sanderson/Mistborn/
├── 01 - The Final Empire/
│   ├── MISTBORN0101P01.mp3 ... MISTBORN0103P07.mp3   (19 files)
│   └── cover.jpg
├── 02 - The Well of Ascension/
│   ├── Brandon Sanderson - Mistborn 02 - The Well of Ascension pt 1..3.mp3
│   └── cover.jpg
├── 03 - The Hero of Ages/
│   ├── MISTBORN0301..0303.mp3
│   └── cover.jpg
├── 04 - The Alloy of Law/
│   ├── MISTBORN04P01..P07.mp3
│   └── cover.jpg
├── 05 - Shadows of Self/
│   ├── MISTBORN05P01..P08.mp3
│   └── cover.jpg
├── 06 - The Bands of Mourning/
│   ├── MISTBORN0601P01..0602P05.mp3  (10 files)
│   └── cover.jpg
├── 07 - The Lost Metal/
│   ├── Brandon Sanderson - The Lost Metal꞉ A Mistborn Novel.m4b
│   └── cover.jpg
└── 08 - Mistborn: Secret History, The Eleventh Metal, and Allomancer Jak and the Pits of Eltania/   (or "Secret History & Short Stories")
    ├── Mistborn- Collected Tales.m4a
    └── cover.jpg   (to be fetched — none exists)
```

**Key changes vs today:**
1. **Move the loose `Mistborn- Collected Tales.m4a` into its own book subfolder** — this single action un-breaks the ABS grouping.
2. **Add numeric `NN - ` prefixes to book folders** — ABS derives series sequence from the book folder prefix (`getSequence()`). Required so the series sorts Final Empire → Lost Metal instead of alphabetically.
3. **Author folder `Brandon Sanderson/` stays as-is.**

### Series sequence rationale
| Position | Folder | Publication year |
|---|---|---|
| 1 | The Final Empire | 2006 |
| 2 | The Well of Ascension | 2007 |
| 3 | The Hero of Ages | 2008 |
| 4 | The Alloy of Law | 2011 |
| 5 | Shadows of Self | 2015 |
| 6 | The Bands of Mourning | 2016 |
| 7 | The Lost Metal | 2022 |
| 8 | Secret History + Eleventh Metal + Allomancer Jak (Collected Tales) | 2016/2018 |

*(Option: user may prefer Secret History positioned separately as a companion/novella rather than slot 8. Confirm at approval.)*

---

## 4. Metadata Plan

ABS metadata precedence (this library, from `libraries.settings`): `["folderStructure", "audioMetatags", "nfoFile", "txtFiles", "opfFile", "absMetadata"]` — later entries override earlier; `metadata.json` (`absMetadata`) is **highest**.

### 4a. Folder structure (baseline)
- Author = `Brandon Sanderson` (from top folder) ✓
- Series = `Mistborn` (from `Mistborn/`) ✓
- Book title = book folder minus the `NN - ` prefix ✓
- Sequence = from `NN - ` prefix ✓

### 4b. Embedded audio tags (already present, mostly helpful)
ABS reads the **first sorted audio file's** tags for series/title when folder structure doesn't provide them. Current tags are inconsistent (verbose album text, GraphicAudio artist, duplicate track numbers). **Do not rely on them** — folder structure + metadata.json will override.

### 4c. metadata.json (recommended: authoritative)
Place a `metadata.json` in each book folder (and the series folder) so ABS metadata is deterministic regardless of the messy source tags.

Per-book `metadata.json` schema (top-level keys; `series` as array of `"Name #N"` strings):
```json
{
  "title": "The Final Empire",
  "subtitle": "",
  "authors": ["Brandon Sanderson"],
  "narrators": [],
  "series": ["Mistborn #1"],
  "genres": ["Fantasy"],
  "tags": [],
  "publishedYear": "2006",
  "publisher": "Tor Books",
  "description": "",
  "language": "en",
  "explicit": false,
  "abridged": false
}
```
Series-level `metadata.json` (in `Mistborn/`) — if ABS accepts it — can carry the series description. **Note:** docs-lookup confirmed series-level `metadata.json` is **ignored** by ABS (only book-folder level is read via `metadataJsonLibraryFile`); a series description would have to be edited in the ABS UI instead. Flagged as optional follow-up.

### 4d. Track ordering within each book
ABS orders tracks via `runSmartTrackOrder` (meta track → filename). Two books have duplicate/ambiguous track tags:
- **Hero of Ages**: all 3 files tag `track=1` → ABS must fall back to filename (`MISTBORN0301..303` sorts fine).
- **Bands of Mourning**: two discs, each `track=1..5` → same filenames would collide. `MISTBORN0601P01..05` + `0602P01..05` sort correctly by filename.
- **The Final Empire**: no tags; filenames `0101P01..0103P07` sort correctly.
- **Well of Ascension**: tag track 1–3 OK.

**Decision needed:** The filename sort is already correct for all books (verified by the natural MISTBORN**NN** ordering). Optionally embed proper track numbers during execution for robustness — this is a **nice-to-have**, not required for correctness. Recommend: skip tag rewriting initially; verify ordering in ABS after rescan.

### 4e. Covers
- All 7 numbered books have usable covers (each folder's `cover.jpg`). Hero of Ages has 5 covers — ABS picks one; cleanup (delete redundant `cover - mistborn0301_2.jpg` etc.) is optional.
- **Collected Tales book has NO cover** — must fetch one (e.g. from OpenLibrary/Google Books for "Mistborn: Secret History and The Eleventh Metal" or the Arcanum Unbounded cover) before rescan, else ABS falls back to embedded/empty.
- Lost Metal folder has 2 stray `Capture*.PNG` screenshots + `516+n7OsZNL.jpg` — clean these (keep the proper "A Mistborn Novel.jpg" as cover).

---

## 5. Execution Steps (pending approval)

All on `roadman` via SSH. **No re-transfer from source needed — all audio already on remote and verified.**

### Phase 1 — Restructure folders (single remote shell script, dry-run first)
1. Create book dirs with `NN - ` prefixes.
2. `mv` each existing book folder's audio+cover into the numbered dir (or rename the existing dirs in place with the prefix).
3. Create the Collected Tales book dir; move the loose `Mistborn- Collected Tales.m4a` into it.
4. Move the 4 Lost Metal extra images out / delete strays.
5. (Optional) delete redundant Hero of Ages covers.
6. Verify: no audio files remain directly in `Mistborn/`; each book dir has exactly its expected files; `find ... -maxdepth 1 -type f` on `Mistborn/` returns only dirs + metadata.json.

### Phase 2 — Write metadata.json files
- Generate 8 per-book `metadata.json` files (titles/series/sequence/years per §3/§4).
- Place each in its book folder.

### Phase 3 — Fetch cover for Collected Tales
- Download cover art to `08 - …/cover.jpg` (and set `chmod 644`).

### Phase 4 — Reset ABS state
Because the item is currently merged, ABS **will not** auto-split it reliably:
1. **Delete the merged item in ABS UI** (or via API `DELETE /api/items/ece022ba-d5ec-4461-a4a0-73cf4197052f`).
   - ⚠️ This **removes any listening progress** tied to the merged item (per ABS docs).
2. **Full library rescan**: UI "Scan" or `POST /api/libraries/a7ee8f7e-8726-47ad-bc06-d1763d4c6d85/scan?force=1`.
   - Do **not** rely on the file watcher for this structural change (watcher ignores parent-dir-of-item moves).

### Phase 5 — Verify
1. `sqlite3` query ABS DB: expect **8** libraryItems under `%Mistborn%`, one `series` row "Mistborn", `bookSeries` sequences 1–8, author = Brandon Sanderson.
2. Confirm each book's `audioFiles` count and total duration:
   | Book | files | approx duration |
   |---|---|---|
   | Final Empire | 19 | ~15h |
   | Well of Ascension | 3 | ~7h |
   | Hero of Ages | 3 | ~22h |
   | Alloy of Law | 7 | ~8h |
   | Shadows of Self | 8 | ~9h |
   | Bands of Mourning | 10 | ~12h |
   | Lost Metal | 1 | ~7h |
   | Collected Tales | 1 | ~2h |
3. Spot-check track order in ABS for Hero of Ages, Bands of Mourning, The Final Empire.
4. Check covers render for all 8 books.
5. Optional: `--verify` with the audioTransfer tool is not applicable here (manual restructure); rely on DB checks + `find` file counts.

---

## 6. Risks & Rollback

| Risk | Mitigation |
|---|---|
| Deleting merged item loses listening progress | Confirm acceptable before execution; document it |
| Rescan picks up wrong title/author | `metadata.json` is highest precedence → deterministic |
| Filename track ordering ambiguous | Verify after rescan; if wrong, embed track tags (additive, safe) |
| Wrong sequence | `NN - ` prefix drives sequence; verify in DB |
| Lost Metal stray screenshots deleted | They're visual noise; nothing references them |
| Collected Tales cover unavailable | Fall back to no-cover (ABS will show blank) or use series-appropriate art |
| Rollback | All steps are renames/additions EXCEPT deleting the merged ABS item + 4 stray images. A filesystem snapshot (`mv` to a `.bak`) of the 4 images is trivial. Keep a copy of current ABS DB before rescan (`cp absdatabase.sqlite absdatabase.sqlite.bak`) |

---

## 7. Out of Scope / Follow-ups (noted, not planned)

- **Other libraries with the same loose-file hazard** (found in sweep): `Colleen Hoover/Nur Noch Ein Einziges Mal (It Ends With Us, German)/…` (loose mp3 + 1 subdir), `Travis Baldree/Legends and Lattes` (loose + 1 subdir), `Shelby Mahurin/Serpent & Dove` (loose + 1 subdir). Each may be an author-level loose-file merge — verify separately.
- **Series descriptions** for Mistborn (set in ABS UI).
- **Legacy Python implementation** — unaffected.
- **Other series ordering audits** — the Mistborn fix pattern (`NN - ` prefixes) can be applied to other multi-book series later if desired.

---

## 8. Approval Checklist

- [x] Confirm series position for Secret History (slot 8 vs standalone). → **Slot 8**
- [x] Confirm deleting merged ABS item (and its progress) is OK. → **Approved**
- [x] Confirm fetching an external cover for Collected Tales is OK (or skip). → **Fetched from OpenLibrary**
- [x] Confirm cleanup of redundant covers / stray images in Hero of Ages + Lost Metal. → **Done**
- [x] Then I execute Phase 1–5 and report verification evidence. → **Executed 2026-08-05**
