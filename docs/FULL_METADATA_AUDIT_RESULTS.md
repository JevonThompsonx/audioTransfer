# Full Library Metadata Audit — Results

**Date:** 2026-08-11 (late session)
**Scope:** Entire Audiobookshelf library on `roadman` (965 books → 964 after dedup, 194 → 195 series, 459 → 423 authors).
**Plan:** `docs/FULL_METADATA_AUDIT_PLAN.md`
**Backup (pre-audit):** `/mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-fullmeta-20260811-214706`
**API:** `http://roadman:13378` (host-published), JWT from DB `server-settings.tokenSecret`.
**Goals:** Correct title, authors, series (registered + numbered), and useful day-to-day metadata (description, cover, publisher, year, language).

---

## Summary

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| Books (items) | 965 | 964 | −1 (duplicate Under Her Spell) |
| Series | 194 | 195 | +1 (Nemesis split) |
| Authors | 459 | 423 | −36 (merged splits, purged empty/orphan records) |
| `bookSeries` links | 533 | 533 | 0 |
| Books with title == series name (anomaly) | 296 | 29 (all legitimate) | −267 |
| Titles with `(Unabridged)` suffix | 36 | 0 | −36 |
| Titles with `[brackets]` / `Book N` suffix | ~34 | 0 | −34 |
| Books missing description | 731 | 54 | −677 |
| Books missing cover | 6 | 0 | −6 |
| Books missing publisher | 804 | 35 | −769 |
| Books missing year | 405 | 26 | −379 |
| Books missing language | 443 | 21 | −422 |
| `isMissing` / `isInvalid` items | 0 / 0 | 0 / 0 | 0 |
| Blank series sequences (excl. `Various` anthologies) | 26 | 0 | −26 |
| Orphaned `bookAuthors` links | 138 | 0 | −138 |
| Narrator/translator/illustrator entries in author lists | ~20 | 0 | −20 |
| Split/duplicate author records | 8 | 0 | −8 |

**Result:** **894 of 964 books (93%)** now have every core metadata field populated (title, authors, description, publisher, year, language, cover). The 54 books still missing descriptions are genuinely obscure titles (self-published dump erotica, radio-drama editions, anthology short pieces) with no provider data — flagged, not guessed. 23 of those have all fields except description.

---

## What was done

### Phase 2 — Title fixes (A + B)

**Backup:** `absdatabase.sqlite.bak-fullmeta-p2-20260811-220800`

- **A (282 extractable title==series):** For every book whose DB title equalled its series name (root cause: corrupt `metadata.json`), extracted the real title from the folder name using the `<Series> <seq> <Title>` regex and applied `PATCH /api/items/:id/media` (title). Series + sequence preserved.
  - e.g. `Cain Casey` → `The Devil Inside`; `Beyond Fairytales` → `Taliasman`; `Romance` → `Passion's Bright Fury`.
- **B (66 title pollution):** stripped `(Unabridged)` (36), series-suffix `Book N` / `- Series, Book N` / `Bk N` (27), `[brackets]` (7, e.g. `Assistant to the Villain [Assistant to the Villain, Book 1]` → `Assistant to the Villain`), an embedded-newline title (`Strangers:\nA Memoir of Marriage` → `Strangers: A Memoir of Marriage`), and a Turkish-language title on an English book (`Beşinci Anlaşma [The Fifth Agreement]` → `The Fifth Agreement`).
  - Cinder/Cress and the Goode Brothers books had BOTH `(Unabridged)` and a series-suffix — both stripped.
- **22 subtitle series-suffix fixes:** cleared redundant `"X, Book N"` subtitles (e.g. `Haunting Danielle Series, Book 20`); set Fifth Agreement subtitle to `A Practical Guide to Self-Mastery`.
- **14 "special" title==series cases** (Hatchet, The Gilded Wolves, Serpent & Dove, Branding Her omnibuses, Best Lesbian Erotica/Romance annuals, etc.) were verified as **legitimate series-titled books** (book-1-titled or anthologies) — left unchanged.
- **metadata.json rewritten** on disk for all 339 changed books (312 updated existing, 27 created new) so a future rescan cannot revert.

**Totals:** 348 title fixes applied via API (339 real changes + 9 legit no-ops), 22 subtitle fixes, 0 failures.

**Verification:** title==series count 296 → 29 (all legitimate); `Unabridged` 36 → 0; brackets 0; `Book N` suffixes 0. Light-novel `Vol. N` titles (Shield Hero, Goblin Slayer, Classroom of the Elite, Ascendance of a Bookworm, That Time I Got Reincarnated as a Slime, So I'm a Spider) left intact — those volume numbers are the real book titles.

### Phase 3 — Author fixes (C + D)

**Backup:** `absdatabase.sqlite.bak-fullmeta-p3-20260811-221338`

- **Pruned non-author roles from author lists (API PATCH, 30 books):**
  - Narrators: Kurt Kanazawa, Matthew Bridges (Shield Hero), Romy Nordlinger (Haunting Danielle), James Konicek (Zodiac Academy), Natalie Naudus (A Song to Drown Rivers), Orlagh Cassidy (Beagle), Meg Sylvan / Aiden Snow / Teddy Hamilton / Tristan Morris (Blood of Hercules).
  - Translators/illustrators: Kevin Gifford (Slime), Noboru Kannatuki + Kevin Steinbach (Goblin Slayer), Quof (Bookworm), Jenny McKeon McKeon (Spider), Tsukasa Kiryu (Spider), Mitz Mitz Vah (Slime), David Allen Sibley – introduction, Bayo Akomolafe – foreword.
  - **Narrator-as-only-author → real author:** Cameron Sullivan → Henry H. Neff (`The Red Winter`), Reba Buhr → Miya Kazuki (Bookworm 01.1 + 01.2), narrators → Kumo Kagyu (Goblin Slayer Vol 9).
  - **Split/duplicate merges:** `Kurt | Vonnegut` → `Kurt Vonnegut` (Slaughterhouse-Five), `Kurt Vonnegut Jr` → `Kurt Vonnegut` (Timequake), `Jenny Lawson/Jenny Lawson` → `Jenny Lawson` (Furiously Happy).
- **Investigated 4 suspected narrator-misattributions — all were NO-OPs (DB already correct):**
  - `Between Life and Death`, `Between Sun and Moon`, `Between the Moon and Her Night` are all genuinely authored by **Jaclyn Kot** (her "Between Life and Death Series", verified via Audible). The Bulgakov-folder copy of book 3 is a folder misplacement — flagged, not moved (folder restructure out of scope).
  - `An Amateur Witch's Guide to Murder` is genuinely by **K. Valentin** (verified via Penguin Random House / Goodreads / Google Books). The `Kiri Callagan` folder is misnamed — flagged, not moved.
- **DB author-record cleanup** (container stopped, journal mode = delete):
  - Merged `don Miguel Ruiz` → `Don Miguel Ruiz` (re-pointed The Four Agreements).
  - Deleted 138 orphaned `bookAuthors` links (books deleted long ago).
  - Deleted 16 empty/orphan author records (split `Kurt`+`Vonnegut`, `Kurt Vonnegut Jr`, `Jenny Lawson/Jenny Lawson`, narrator-only records, combined `Bobbi Holmes/Anna J. McIntyre/Romy Nordlinger`, `Rebecca Ross/Alex Wingfield`, etc.).
- **metadata.json rewritten** for all 31 author-changed books.

**Verification:** 0 role-marked/narrator authors remain; author count 459 → 423; orphaned `bookAuthors` 138 → 0.

### Phase 4 — Series fixes (E + F + J)

**Backup:** `absdatabase.sqlite.bak-fullmeta-p4-20260811-222010`

- **E — blank sequences (22 applied via API):**
  - **Haunting Danielle:** 18 blank sequences filled → series now complete 1–25 in publication order.
  - **Last Unicorn** #1, **Embodied Activism** #1, **Rick Riordan Presents** #1 (The Spirit Glass), **Foundryside** #1 (series renamed from `Foundryside Founders` — the folder wrapper; real series is `Foundryside`).
  - **So I'm a Spider Vol 16:** set seq 16, removed bogus second series `So What? (light novel)`, cleaned `(light novel)` title suffix.
- **F — cross-author collisions:**
  - **Nemesis split:** April Daniels (`Dreadnought` #1, `Sovereign` #2) → series **`Nemesis`**; K.A. Kron (`Injustice` #1, `Blind Justice` #2) → series **`Nemesis (K.A. Kron)`**. Required a direct DB split (the `series` table has `UNIQUE(name, libraryId)` — two same-named rows are impossible). `metadata.json` updated for all 4.
  - **Under Her Spell dedup:** true duplicate (identical cover md5 `fe3f8961…`, duration 25325s both, no listening progress). Deleted the solo `Bridget Essex` copy via API; quarantined the on-disk folder to `/mnt/media/audiobooks/.quarantine-dup/Under-Her-Spell-dupe-bridget-essex` (so a rescan cannot resurrect it). Kept the co-author `Elora Bishop & Bridget Essex` copy.
- **J — series registration:** Verified every series-structured folder (depth 4–6) is linked. Multi-book authors (Paris Rivera 13, Gerri Hill 13, Georgia Beers 13, Karis Walsh, Melissa Brayden, etc.) were checked: their standalone books have **no** series subfolder and no provider series — genuinely standalone (the 2026-08-05 audit already purged the bogus author-as-series rows). Real series (Study Breaks, Hunter, Ross & Sullivan, Puppy Love Romance, Music of the Soul short stories) were already correctly registered.
- **metadata.json rewritten** for 22 series-fixed + 4 Nemesis + 1 Spider books.

**Remaining legitimate (documented, not errors):**
- 4 `Various` anthology entries with blank sequence (Best Lesbian Erotica/Romance annuals — unnumbered by design).
- `The Farseer: Assassin's Apprentice` in 2 series (Realm of the Elderlings + Farseer Trilogy) — intentional franchise design.
- Duplicate sequence 3 in Girls of Paper and Fire (Part I/II), sequence 4 in Shield Hero (two distinct Vol-4 rips), sequence 3 in Zodiac Academy (Part 1 of 2 / Part 2 of 2) — legitimate part-numbering.

### Phase 5 — Enrichment (G + H + I)

**Backup:** `absdatabase.sqlite.bak-fullmeta-p5-20260811-224435`

- **G — Descriptions (712 → 54 missing):** Provider search (Audible primary; Google + OpenLibrary fallback where the ABS container could reach them) with **strict title + volume-aware matching** — only applied when the provider title matched the current title (exact, series-prefix, or ≥60% token overlap with volume agreement). Applied via `PATCH` of `description` only; title/authors/series never touched.
  - **4 wrong matches detected during a targeted pass** (Hood, Carmilla, Slow River, Legends and Lattes — the provider returned a different book): descriptions were **cleared to null** immediately, per the "never guess" rule. Correct descriptions later applied to Eon, Heavenly Tyrant, The Newt and Demon, Kissing the Witch, The Price of Salt, When Women Were Warriors, Rage, She Comes First, Legends and Lattes.
  - Remaining 54: no provider data exists (obscure self-published dump erotica, radio-drama editions, Robin Hobb anthology short pieces, Mistborn Collected Tales).
- **H — Covers (6 → 0):** Downloaded `cover.jpg` from the Audible CDN into the book folders for Scarlet, Cress, Cinder, I Will Never Leave You, Assistant to the Villain, Eon: Dragoneye Reborn. `coverPath` updated via `PATCH` (verified via `media.coverPath`).
- **I — Publisher/Year/Language (804/405/443 → 35/26/21):** Filled from Audible + OpenLibrary matches; language defaulted to `English` only on confirmed English matches.
- **metadata.json rewritten** for all 838 phase-5-touched books (full DB sync: title, subtitle, authors, series, description, publisher, year, language, coverPath, narrators, genres, tags). Three trailing-space path variants handled manually.

**Verification:** sampled 12 enriched descriptions — all match their titles. Cover count 0 missing.

### Phase 6 — Verification (all)

- **Q0 sanity:** 964 books / 964 items / 195 series / 423 authors / 533 `bookSeries` links / 0 `isMissing` / 0 `isInvalid`.
- **Re-ran every anomaly query:**
  - title==series: 29 (all legitimate)
  - `Unabridged`: 0 · brackets: 0 · `Book N` suffix: 0
  - role-marked authors: 0 · split/dup author records: 0
  - blank sequences (excl. `Various`): 0 · orphan `bookSeries` links: 0 · orphan `bookAuthors` links: 0
  - books in 2+ series: 1 (documented franchise intent) · duplicate seq pairs: 3 (all documented)
- **Spot-checked 25 books via API GET** (titles, authors, series, covers, descriptions) across every fix category — all correct, including the split Nemesis series and the 6 new covers.
- **metadata.json ↔ DB consistency:** 11/11 sampled books match on disk vs API (title, authors, series).
- **metadata.json coverage:** every touched book has a `metadata.json` on disk so a rescan cannot revert any fix.

---

## Rollback

```bash
# Restore the pre-audit DB (container stopped):
docker stop audiobookshelf
cp /mnt/docker/audiobookshelf/config/absdatabase.sqlite.bak-fullmeta-20260811-214706 \
   /mnt/docker/audiobookshelf/config/absdatabase.sqlite
docker start audiobookshelf
```

Per-phase backups also exist if a partial rollback is preferred:
- `absdatabase.sqlite.bak-fullmeta-p2-20260811-220800` (pre-title-fixes)
- `absdatabase.sqlite.bak-fullmeta-p3-20260811-221338` (pre-author-fixes)
- `absdatabase.sqlite.bak-fullmeta-p4-20260811-222010` (pre-series-fixes)
- `absdatabase.sqlite.bak-fullmeta-p5-20260811-224435` (pre-enrichment)

**Note:** rewritten `metadata.json` files are on disk and are NOT covered by the DB backup. Rollback of those would require file-level restore (they pin the corrected title/authors/series and will re-assert on the next rescan — that is the intended behavior).

**Also note:** the deduplicated Under Her Spell folder is quarantined at `/mnt/media/audiobooks/.quarantine-dup/Under-Her-Spell-dupe-bridget-essex` (not deleted) and can be restored by moving it back.

---

## Books not fixed / flagged

| Book | Why | Action |
|------|-----|--------|
| 54 obscure books (dump erotica, radio dramas, short pieces) | No provider description exists | Flagged; left with whatever title/author/series they have |
| `Between the Moon and Her Night` (folder `/Михаил Афанасьевич Булгаков/`) | Author is Jaclyn Kot, but book sits in a Bulgakov folder (folder misplacement) | Author correct in DB; folder move out of scope — flagged for user |
| `An Amateur Witch's Guide to Murder` (folder `Kiri Callagan/`) | Real author K. Valentin; folder misnamed | Author correct in DB; folder move out of scope — flagged for user |
| Mistborn Collected Tales | Obscure anthology edition; no provider match | Flagged |
