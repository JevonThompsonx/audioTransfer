# FULL METADATA AUDIT — RUN LOG

**Date:** 2026-08-11 (late session)
**Host:** roadman — ABS API http://roadman:13378 (host-published), DB /mnt/docker/audiobookshelf/config/absdatabase.sqlite
**Library:** a7ee8f7e-8726-47ad-bc06-d1763d4c6d85 (965 books / 194 series / 459 authors)
**Baseline backup:** absdatabase.sqlite.bak-fullmeta-20260811-214706
**Token method:** JWT signed locally with server-settings tokenSecret (container jsonwebtoken path broken).

## Phase 0 — recon + prep
- Verified SSH, sqlite3 (3.46.1), API on 13378, direct curl from workstation works.
- Fresh token generated (HMAC HS256 with tokenSecret from DB).
- Loaded Phase-1 prepared fixes from /tmp/opencode/audit/ and validated against CURRENT DB:
  - title_fixes.json: 321 entries (282 title_eq_series + 36 unabridged + 3 series_prefix) — all old_title match current DB ✓
  - author_fixes.json: 32 entries — 28 real + 4 placeholders (investigated below) ✓
  - series_fixes.json: 22 entries ✓
- Investigated 4 author placeholders via Audible search: 
  - "Between Life and Death" (in Dolores Cannon folder) = Jaclyn Kot "Between Life and Death Series, Book 1" (author Jaclyn Kot) — DB author already correct.
  - "Between Sun and Moon" = Jaclyn Kot, Book 2 — correct.
  - "Between the Moon and Her Night" (in Bulgakov folder!) = Jaclyn Kot, Book 3 — DB author correct, folder is misplaced (flagged, no move per rules).
  - "An Amateur Witch's Guide to Murder" = K. Valentin (confirmed PRH/Goodreads/Google Books) — DB author correct. Folder "Kiri Callagan" is wrong (flagged).
  - => The 4 placeholders are NO-OPS (authors already correct). NOT applied.
- Found additional title pollution NOT in prepared lists: 27 books (Eon, Gods&Monsters, Shield Hero vols w/o uniform vol format handled separately, Haunting Danielle Bk17/18 + "- Series, Book 20/21/22/24", Fitzpatrick Christmas, Assistant to the Villain, Girls of Fate and Fury Part I/II, Caught Up, Red Rising, Eyes on Me, Keep Me, Promise Me, Good Girl Effect, Blood & Honey, Caraval, Catching Fire, Mockingjay, Harrow, Nona, Fifth Agreement, Strangers newline, Wrath of the Fallen). Added as extra_pollution fixes.
- Cinder/Cress/Goode Brothers had both Unabridged AND series-suffix — overrode prepared new_title to strip both.
- 14 "special" title_eq_series cases = legitimate series-titled (book 1s / omnibuses / anthologies): no title change.
- Total title fixes planned: 348.

## Phase 2 — Title fixes
(see below as executed)

## Phase 2 — Title fixes (EXECUTED) ✓
- Backup: absdatabase.sqlite.bak-fullmeta-p2-20260811-220800 (verified 15118336 bytes)
- Applied 348 title fixes via PATCH /api/items/:id/media (title): 343 changed + 5 pre-existing clean (re-run idempotent), 0 failures.
  - 282 title_eq_series (real title extracted from folder regex)
  - 36 unabridged_suffix stripped
  - 3 series_prefix_title (Elite Operatives->Lethal Affairs, Caraval Book 3->Finale, Haunting Danielle Bk16->The Ghost and the Doppelganger)
  - 27 extra_pollution (Eon, Book of Azrael, Wrath of the Fallen, HD 17/18/20/21/22/24, Assistant to the Villain, Girls of Fate and Fury Part I/II, Caught Up, Red Rising, Eyes on Me, Keep Me, Promise Me, Good Girl Effect, Blood & Honey, Caraval, Catching Fire, Mockingjay, Harrow, Nona, Fifth Agreement, Strangers newline, Snowed In)
- 9 legit no-ops (book 1 titled same as its series: Beebo Brinker 00, Jericho, Karen Memory, Music of the Soul, Santa Olivia, The Lost Girls, Harmony, Tats, Afterworlds)
- 22 subtitle series-suffix fixes via PATCH (subtitle cleared where "X, Book N" redundant; Fifth Agreement subtitle set "A Practical Guide to Self-Mastery")
- Rewrote metadata.json on disk for all 339 changed books (312 updated existing, 27 created new) via host-side python script reading DB directly. Precedence preserved (chapters kept where present).
- VERIFIED: title==series-name count down to 28 (all legitimate series-titled book-1/omnibus cases); 0 Unabridged; 0 brackets; 0 series-suffix pollution. Light-novel "Vol. N" titles (Shield Hero, Goblin Slayer, COTE, Slime, Bookworm, Spider) left intact — they are the real book titles.
- Spot-checked 7 books via API GET: titles/series/subtitles correct.

## Phase 3 — Author fixes (C+D) (EXECUTED) ✓
- Backup: absdatabase.sqlite.bak-fullmeta-p3-20260811-221338 (verified)
- Applied 30 author fixes via PATCH /api/items/:id/media (metadata.authors):
  - Pruned narrators/translators/illustrators/foreword-writers: Kurt Kanazawa, Matthew Bridges (Shield Hero), Romy Nordlinger (HD), James Konicek (Zodiac), Kevin Gifford (Slime), Noboru Kannatuki + Kevin Steinbach (Goblin Slayer), Quof (Bookworm), Jenny McKeon McKeon (Spider), David Allen Sibley (Backyard Birds), Bayo Akomolafe (Embodied Activism), Orlagh Cassidy (Beagle), Natalie Naudus (Drown Rivers), Meg Sylvan/Aiden Snow/Teddy Hamilton/Tristan Morris (Blood of Hercules), Tsukasa Kiryu (Spider), Mitz Mitz Vah (Slime)
  - Narrator-as-only-author -> real author: Cameron Sullivan->Henry H. Neff (Red Winter), Kumo Kagyu (Goblin Slayer Vol 9), Reba Buhr->Miya Kazuki (Bookworm 01.1 + 01.2) [2 NEW beyond prepared list]
  - Merged split/dup: "Kurt|Vonnegut"->Kurt Vonnegut (Slaughterhouse-Five), "Kurt Vonnegut Jr"->Kurt Vonnegut (Timequake), "Jenny Lawson/Jenny Lawson"->Jenny Lawson (Furiously Happy)
- Verified: 4 "placeholders" in prepared list were NO-OPs (authors already correct):
  - Between Life and Death / Between Sun and Moon / Between the Moon and Her Night = Jaclyn Kot (real author of Between Life and Death series; Bulgakov folder is misplaced — flagged)
  - An Amateur Witch's Guide to Murder = K. Valentin (real author, confirmed PRH/Goodreads/Google Books; "Kiri Callagan" folder is misnamed — flagged)
- DB author-record cleanup (container stopped, journal mode=delete):
  - Merged "don Miguel Ruiz"->"Don Miguel Ruiz" (The Four Agreements repointed)
  - Deleted 138 orphaned bookAuthors links (books deleted long ago)
  - Deleted 16 empty/orphan author records (incl. split "Kurt"+"Vonnegut", "Kurt Vonnegut Jr", "Jenny Lawson/Jenny Lawson", narrator records Reba Buhr/Robert Fass/Quof, combined "Bobbi Holmes/Anna J. McIntyre/Romy Nordlinger", "Rebecca Ross/Alex Wingfield", etc.)
  - Authors 459 -> 423. Orphaned bookAuthors 138 -> 0.
- Rewrote metadata.json for all 31 author-changed books (full regen from DB with corrected authors).
- VERIFIED: 0 role-marked/narrator authors remain (only legit "K. Valentin"); author page consolidation done.

## Phase 4 — Series fixes (E+F+J) (EXECUTED) ✓
- Backup: absdatabase.sqlite.bak-fullmeta-p4-20260811-222010 (verified)
- E) Blank sequences: applied 22 series fixes via PATCH /api/items/:id/media (metadata.series with sequence):
  - Haunting Danielle: 18 blank -> sequences 1-25 complete (publication order verified)
  - Last Unicorn #1, Embodied Activism #1, Rick Riordan Presents #1 (Spirit Glass), Foundryside #1 (renamed from "Foundryside Founders")
  - So I'm a Spider Vol 16: set seq 16, removed bogus second series "So What? (light novel)", cleaned title "(light novel)" suffix
- F) Cross-author collisions:
  - NEMESIS SPLIT: April Daniels (Dreadnought #1, Sovereign #2) -> series "Nemesis"; K.A. Kron (Injustice #1, Blind Justice #2) -> series "Nemesis (K.A. Kron)". Direct DB split (series UNIQUE(name,libraryId) prevents same-name rows). metadata.json updated for all 4.
  - UNDER HER SPELL dedup: true duplicate (identical cover md5 fe3f8961..., duration 25325s both, no listening progress). Deleted solo "Bridget Essex" copy via API; quarantined on-disk folder to /mnt/media/audiobooks/.quarantine-dup/ (so rescan can't resurrect). Kept co-author "Elora Bishop & Bridget Essex" copy. Books 965 -> 964.
- J) Series registration: verified all series-structured folders (depth 4-6) are linked. Multi-book authors (Paris Rivera, Gerri Hill, Georgia Beers, Karis Walsh, etc.) standalone books confirmed standalone via Audible provider + flat folder structure (no series subdirs). Genuine series (Study Breaks, Hunter, Ross & Sullivan, Puppy Love Romance, Music of the Soul short stories) already registered. No new registrations warranted.
- Remaining legit (documented): "Various" anthology blanks (4), Farseer double-series (franchise intent), Girls of Paper and Fire seq3 Part I/II, Shield Hero Vol 4 dual rips, Zodiac Academy seq3 parts.
- Rewrote metadata.json for 22 series-fixed + 4 Nemesis + 1 Spider books.

## Phase 5 — Enrichment (G+H+I) (EXECUTED) ✓
- Backup: absdatabase.sqlite.bak-fullmeta-p5-20260811-224435 (verified)
- Description enrichment (G): 712 missing -> 54 (filled 658 via provider search, strict title+volume-aware matching)
  - 4 WRONG matches detected during targeted pass (Hood, Carmilla, Slow River, Legends and Lattes) — descriptions CLEARED to null immediately (rules: never guess). Correct descriptions later applied to Eon, Heavenly Tyrant, Newt and Demon, Kissing the Witch, Price of Salt/Carol, When Women Were Warriors, Rage, She Comes First, Legends and Lattes.
  - Remaining 54 no-desc: genuinely obscure (dump erotica, radio dramas, Robin Hobb anthology short pieces, Mistborn Collected Tales) — no provider data; flagged.
- Covers (H): 6 missing -> 0. Downloaded cover.jpg from Audible CDN for Scarlet, Cress, Cinder, I Will Never Leave You, Assistant to the Villain, Eon: Dragoneye Reborn. coverPath updated via PATCH (verified 0 missing).
- Publisher/Year/Language (I): 785/405/443 missing -> 35/26/21. Filled via audible + openlibrary provider search (language defaulted 'English' only on confirmed English matches).
- Rewrote metadata.json for all 838 phase-5-touched books (full DB sync: title, subtitle, authors, series, description, publisher, year, language, coverPath, narrators, genres, tags). 3 trailing-space path edge cases handled manually.
- VERIFIED: sampled 12 enriched descriptions all match titles.

## Phase 6 — Verification (EXECUTED) ✓
- Q0 sanity: 964 books / 964 items / 195 series / 423 authors / 533 bookSeries links / 0 isMissing / 0 isInvalid
- All anomaly classes verified clean:
  - A) title==series: 29 (all legitimate — book-1-titled, omnibuses, anthologies; incl. Foundryside renamed series)
  - B) Unabridged=0, brackets=0 (normalized The Reckoning Part 1/2), Book N suffix=0
  - C) role-marked authors=0
  - D) split/dup author records=0
  - E) blank sequences (excl Various)=0
  - F) Nemesis split verified (2 distinct series), Under Her Spell deduped
  - Books in 2+ series: 1 (Farseer franchise intent — documented)
  - Duplicate seq pairs: 3 (all documented legitimate: part-numbering / dual rips)
  - Orphan bookSeries links: 0
- Spot-checks: 17+8 books via API GET — titles, authors, series, covers, descriptions all correct.
- metadata.json coverage: all phase-5-touched books have metadata.json on disk (3 trailing-space path variants verified).
- Quarantine: /mnt/media/audiobooks/.quarantine-dup/Under-Her-Spell-dupe-bridget-essex

## FINAL (Phase 7)
- Pre-audit backup (bak-fullmeta-20260811-214706) re-queried for authoritative baselines: 965 books / 194 series / 459 authors / 731 no-desc / 6 no-cover / 804 no-pub / 405 no-year / 442 no-lang / 138 orphan bookAuthors / 22 blank seq (excl Various).
- Results report written: docs/FULL_METADATA_AUDIT_RESULTS.md
- Runlog copied to docs/FULL_METADATA_AUDIT_RUNLOG.md
- Final state: 964 books / 195 series / 423 authors / 54 no-desc / 0 no-cover / 35 no-pub / 26 no-year / 21 no-lang / 0 orphan / 0 blank seq (excl Various) / 894 fully clean / 0 isMissing / 0 isInvalid.
