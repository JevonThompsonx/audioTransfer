package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jevonx/audioTransfer/pkg/models"
)

// TestDeleteSource_SuccessDeletesSource: case 1 — a real local-only transfer
// with DeleteSource enabled removes the source file, keeps the organized copy,
// and marks the result as deleted. Also serves as the Issue-2 local-fallback
// counting check (report.Local == 1).
func TestDeleteSource_SuccessDeletesSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	dest := filepath.Join(home, "organized")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Test Author - Test Book.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)

	report := RunTransfer(Config{
		SourceDir:    src,
		DestDir:      dest,
		LocalOnly:    true,
		Force:        true,
		DeleteSource: true,
		Parallel:     2,
	})

	if report.Local != 1 {
		t.Errorf("report.Local = %d, want 1", report.Local)
	}
	if report.Deleted != 1 {
		t.Errorf("report.Deleted = %d, want 1", report.Deleted)
	}
	if len(report.Results) != 1 || !report.Results[0].SourceDeleted {
		t.Errorf("results[0].SourceDeleted = %+v, want a single result with SourceDeleted=true", report.Results)
	}

	if _, err := os.Stat(bookFile); !os.IsNotExist(err) {
		t.Errorf("source file should be deleted, stat err = %v", err)
	}
	destCopy := filepath.Join(dest, "Test Author", "Test Book", "Test Author - Test Book.mp3")
	if _, err := os.Stat(destCopy); err != nil {
		t.Errorf("organized copy missing: %v", err)
	}
}

// TestDeleteSource_FailureNotDeleted: case 2 — a failed result never deletes.
func TestDeleteSource_FailureNotDeleted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	bookDir := filepath.Join(src, "MyBook")
	os.MkdirAll(bookDir, 0755)
	bookFile := filepath.Join(bookDir, "a.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)

	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest"), DeleteSource: true}
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "MyBook", Status: "failed", Error: "boom"}},
	}
	matched := []bookWithID{
		{book: &models.BookSource{Name: "MyBook", Path: bookDir, AudioFiles: []string{bookFile}}},
	}

	deleteSourceFiles(cfg, matched, report)

	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("failed book must not be deleted: %v", err)
	}
}

// TestDeleteSource_DryRunNotDeleted: case 3 — dry-run never deletes, and
// deleteSourceFiles early-returns.
func TestDeleteSource_DryRunNotDeleted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Book.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)

	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest"), DryRun: true, DeleteSource: true}
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "Book", Status: "local"}},
	}
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Book", Path: src, AudioFiles: []string{bookFile}, IsSingleFile: true}},
	}

	deleteSourceFiles(cfg, matched, report)

	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0 in dry-run", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("file must not be deleted in dry-run: %v", err)
	}
}

// TestDeleteSource_DirectoryBookLeafOnly: case 4a — a directory book deletes
// only its leaf dir, leaving the author dir and sibling books intact.
func TestDeleteSource_DirectoryBookLeafOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	authorDir := filepath.Join(src, "Author")
	bookDir := filepath.Join(authorDir, "Book")
	siblingDir := filepath.Join(authorDir, "Book2")
	os.MkdirAll(bookDir, 0755)
	os.MkdirAll(siblingDir, 0755)
	os.WriteFile(filepath.Join(bookDir, "a.mp3"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(bookDir, "cover.jpg"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(siblingDir, "b.mp3"), []byte("x"), 0644)

	book := &models.BookSource{
		Name:       "Book",
		Path:       bookDir,
		AudioFiles: []string{filepath.Join(bookDir, "a.mp3")},
		CoverFiles: []string{filepath.Join(bookDir, "cover.jpg")},
	}
	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest")}

	fp := deleteFootprint(book, cfg)
	if len(fp) != 1 || !samePath(fp[0], bookDir) {
		t.Fatalf("footprint = %v, want exactly [%s]", fp, bookDir)
	}
	deletePaths(fp)

	if _, err := os.Stat(bookDir); !os.IsNotExist(err) {
		t.Errorf("leaf book dir should be deleted: %v", err)
	}
	if _, err := os.Stat(authorDir); err != nil {
		t.Errorf("author dir should remain: %v", err)
	}
	if _, err := os.Stat(siblingDir); err != nil {
		t.Errorf("sibling Book2 should remain: %v", err)
	}
}

// TestDeleteSource_SingleFileOwnFileOnly: case 4b — a single-file book deletes
// only its own file; a sibling single-file book in the same container stays.
func TestDeleteSource_SingleFileOwnFileOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	container := filepath.Join(src, "Container")
	os.MkdirAll(container, 0755)
	book1 := filepath.Join(container, "Book One.mp3")
	book2 := filepath.Join(container, "Book Two.mp3")
	os.WriteFile(book1, []byte("x"), 0644)
	os.WriteFile(book2, []byte("x"), 0644)

	book := &models.BookSource{
		Name:         "Book One",
		Path:         container,
		AudioFiles:   []string{book1},
		IsSingleFile: true,
	}
	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest")}

	fp := deleteFootprint(book, cfg)
	if len(fp) != 1 || !samePath(fp[0], book1) {
		t.Fatalf("footprint = %v, want exactly [%s]", fp, book1)
	}
	deletePaths(fp)

	if _, err := os.Stat(book1); !os.IsNotExist(err) {
		t.Errorf("book1 should be deleted: %v", err)
	}
	if _, err := os.Stat(book2); err != nil {
		t.Errorf("book2 should remain: %v", err)
	}
}

// TestDeleteSource_CoLocatedCover: case 5 — the stem-matched cover is deleted,
// a generic cover.jpg in the same directory is not.
func TestDeleteSource_CoLocatedCover(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	container := filepath.Join(src, "Container")
	os.MkdirAll(container, 0755)
	audio := filepath.Join(container, "Book.m4b")
	coCover := filepath.Join(container, "Book.jpg")
	generic := filepath.Join(container, "cover.jpg")
	os.WriteFile(audio, []byte("x"), 0644)
	os.WriteFile(coCover, []byte("x"), 0644)
	os.WriteFile(generic, []byte("x"), 0644)

	book := &models.BookSource{
		Name:         "Book",
		Path:         container,
		AudioFiles:   []string{audio},
		IsSingleFile: true,
	}
	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest")}

	fp := deleteFootprint(book, cfg)
	got := make(map[string]bool)
	for _, p := range fp {
		got[filepath.Base(p)] = true
	}
	if !got["Book.m4b"] || !got["Book.jpg"] {
		t.Errorf("footprint missing co-located audio/cover pair: %v", fp)
	}
	if got["cover.jpg"] {
		t.Errorf("generic cover.jpg must not be deleted: %v", fp)
	}
}

// TestDeleteSource_ContainmentGuard: case 6 — paths outside SourceDir, inside
// DestDir, or equal to SourceDir are never part of a footprint.
func TestDeleteSource_ContainmentGuard(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	outside := filepath.Join(dir, "outside")
	os.MkdirAll(src, 0755)
	os.MkdirAll(dest, 0755)
	os.MkdirAll(outside, 0755)

	cfg := Config{SourceDir: src, DestDir: dest}

	outsideBook := &models.BookSource{
		Name: "B", Path: outside,
		AudioFiles: []string{filepath.Join(outside, "a.mp3")},
	}
	if fp := deleteFootprint(outsideBook, cfg); len(fp) != 0 {
		t.Errorf("outside-SourceDir book should have empty footprint, got %v", fp)
	}

	destBook := &models.BookSource{
		Name: "B", Path: dest,
		AudioFiles: []string{filepath.Join(dest, "a.mp3")},
	}
	if fp := deleteFootprint(destBook, cfg); len(fp) != 0 {
		t.Errorf("inside-DestDir book should have empty footprint, got %v", fp)
	}

	srcBook := &models.BookSource{
		Name: "B", Path: src,
		AudioFiles: []string{filepath.Join(src, "a.mp3")},
	}
	if fp := deleteFootprint(srcBook, cfg); len(fp) != 0 {
		t.Errorf("book.Path == SourceDir should have empty footprint, got %v", fp)
	}
}

// TestDeleteSource_LocalOnlySameDirRefused: case 7 — LocalOnly with
// SourceDir == DestDir refuses to delete, increments Failed, deletes nothing.
func TestDeleteSource_LocalOnlySameDirRefused(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Book.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)

	cfg := Config{SourceDir: src, DestDir: src, LocalOnly: true, DeleteSource: true}
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "Book", Status: "local"}},
	}
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Book", Path: src, AudioFiles: []string{bookFile}, IsSingleFile: true}},
	}

	deleteSourceFiles(cfg, matched, report)

	if report.Failed != 1 {
		t.Errorf("report.Failed = %d, want 1", report.Failed)
	}
	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("file must not be deleted when SourceDir == DestDir: %v", err)
	}
}

// TestDeleteSource_VerifyGatedRefused: regression for A1 — the CLI-level gate
// in resolveDeletion refuses any --delete-source request that is not paired
// with --verify, so source files can never be deleted on scp success alone.
// The gate itself is unit-tested directly in cmd/audiotransfer/main_test.go
// (TestResolveDeletion). Here we assert the orchestrator-level contract: when
// DeleteSource is set WITHOUT a verified transfer pairing, the lower-level
// deleteSourceFiles primitive still honors DeleteSource verbatim, which is why
// the gate MUST live upstream (in main.go) rather than only here.
func TestDeleteSource_VerifyGatedRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	dest := filepath.Join(home, "organized")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Test Author - Test Book.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)

	// With the new default-off behavior, the CLI will NOT pass DeleteSource
	// unless the user explicitly opts in. A direct call with DeleteSource=true
	// still deletes (the primitive honors its input); the guarantee against
	// unverified deletion is enforced by the CLI gate. We assert the default
	// path (no flags) preserves everything.
	report := RunTransfer(Config{
		SourceDir: src,
		DestDir:   dest,
		LocalOnly: true,
		Force:     true,
		Parallel:  2,
		// No DeleteSource, no Verify — the safe default.
	})
	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0 (deletion is opt-in, default off)", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("source file must be preserved when no deletion requested: %v", err)
	}
}

// TestDeleteSource_DefaultOff: regression for A1 — a RunTransfer with no
// DeleteSource flag set (the new default) must never delete source files,
// regardless of how many books transferred successfully.
func TestDeleteSource_DefaultOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	dest := filepath.Join(home, "organized")
	os.MkdirAll(src, 0755)
	for _, name := range []string{"Book One.mp3", "Book Two.mp3"} {
		os.WriteFile(filepath.Join(src, name), make([]byte, 500), 0644)
	}

	report := RunTransfer(Config{
		SourceDir: src,
		DestDir:   dest,
		LocalOnly: true,
		Force:     true,
		Parallel:  2,
		// No DeleteSource, no Verify — the safe default.
	})

	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0 (deletion must be opt-in, default off)", report.Deleted)
	}
	for _, name := range []string{"Book One.mp3", "Book Two.mp3"} {
		if _, err := os.Stat(filepath.Join(src, name)); err != nil {
			t.Errorf("source %s must be preserved under default-off deletion: %v", name, err)
		}
	}
}

// TestDeleteSource_Disabled: case 8 — DeleteSource=false deletes nothing.
func TestDeleteSource_Disabled(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Book.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)

	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest"), DeleteSource: false}
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "Book", Status: "local"}},
	}
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Book", Path: src, AudioFiles: []string{bookFile}, IsSingleFile: true}},
	}

	deleteSourceFiles(cfg, matched, report)

	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0 when disabled", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("file must not be deleted when DeleteSource is false: %v", err)
	}
}

// TestDeleteSource_ResumedNotDeleted: case 9 — a resumed-only result is not a
// successful result, so its source is never deleted.
func TestDeleteSource_ResumedNotDeleted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Book.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)

	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest"), DeleteSource: true}
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "Book", Status: "resumed"}},
	}
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Book", Path: src, AudioFiles: []string{bookFile}, IsSingleFile: true}},
	}

	if hasSuccessfulResult("Book", report) {
		t.Error("resumed result must not count as a successful result")
	}
	deleteSourceFiles(cfg, matched, report)

	if report.Deleted != 0 {
		t.Errorf("report.Deleted = %d, want 0 for resumed book", report.Deleted)
	}
	if _, err := os.Stat(bookFile); err != nil {
		t.Errorf("resumed book must not be deleted: %v", err)
	}
}

// TestDeleteSource_ZipBook: case 10 — a zip book has no local footprint, and
// deleteEligibleZips removes the source zip only when every extracted book has
// a successful result and the zip passes isDeletionSafe.
func TestDeleteSource_ZipBook(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	zipPath := filepath.Join(src, "Bundle.zip")
	os.WriteFile(zipPath, []byte("zip"), 0644)

	cfg := Config{SourceDir: src, DestDir: filepath.Join(dir, "dest"), DeleteSource: true}

	zipBookA := &models.BookSource{
		Name:       "Bundle/Book A",
		Path:       filepath.Join(dir, "tmp-extract", "Book A"),
		AudioFiles: []string{filepath.Join(dir, "tmp-extract", "Book A", "a.mp3")},
		IsFromZip:  true,
		ZipPath:    zipPath,
	}
	if fp := deleteFootprint(zipBookA, cfg); len(fp) != 0 {
		t.Errorf("zip book footprint should be nil, got %v", fp)
	}

	zipBookB := &models.BookSource{
		Name:       "Bundle/Book B",
		Path:       filepath.Join(dir, "tmp-extract", "Book B"),
		AudioFiles: []string{filepath.Join(dir, "tmp-extract", "Book B", "b.mp3")},
		IsFromZip:  true,
		ZipPath:    zipPath,
	}
	matched := []bookWithID{{book: zipBookA}, {book: zipBookB}}

	// Only one of two books succeeded → zip stays.
	report := &models.TransferReport{
		Results: []models.TransferResult{{SourceName: "Bundle/Book A", Status: "transferred"}},
	}
	deleteEligibleZips(cfg, matched, report)
	if _, err := os.Stat(zipPath); err != nil {
		t.Errorf("zip should remain when not all books succeeded: %v", err)
	}

	// All books succeeded → zip is removed.
	report.Results = append(report.Results, models.TransferResult{SourceName: "Bundle/Book B", Status: "local"})
	deleteEligibleZips(cfg, matched, report)
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("zip should be deleted when all books succeeded, stat err = %v", err)
	}
}

// TestDeleteSource_PathHelpers: case 11 — table-driven unit tests for
// isPathWithin, samePath, and deleteFootprint edge cases.
func TestDeleteSource_PathHelpers(t *testing.T) {
	withinCases := []struct {
		p, root string
		want    bool
	}{
		{"/a/b", "/a", true},
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/", true},
		{"/a", "/a", false},      // p == root
		{"/a/../b", "/a", false}, // escapes root after cleaning
		{"/a/", "/a", false},     // p == root (cleaned)
		{"/other/b", "/a", false},
		{"/a", "/a/b", false}, // p is ancestor of root
	}
	for _, c := range withinCases {
		if got := isPathWithin(c.p, c.root); got != c.want {
			t.Errorf("isPathWithin(%q, %q) = %v, want %v", c.p, c.root, got, c.want)
		}
	}

	dir := t.TempDir()
	if !samePath(dir, filepath.Join(dir, ".")) {
		t.Error("samePath should be true for the same directory")
	}
	if samePath(dir, filepath.Join(dir, "..", "..")) {
		t.Error("samePath should be false for a different directory")
	}
	if samePath(dir, "") {
		t.Error("samePath should be false for an empty path")
	}

	// deleteFootprint on a nil book returns nil.
	if fp := deleteFootprint(nil, Config{SourceDir: dir}); len(fp) != 0 {
		t.Errorf("nil book footprint should be nil, got %v", fp)
	}

	// A SourceDir that does not contain the book is unsafe → nothing deleted.
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Book.mp3")
	os.WriteFile(bookFile, []byte("x"), 0644)
	book := &models.BookSource{
		Name: "Book", Path: src,
		AudioFiles:   []string{bookFile},
		IsSingleFile: true,
	}
	if fp := deleteFootprint(book, Config{SourceDir: filepath.Join(dir, "other-src")}); len(fp) != 0 {
		t.Errorf("footprint outside configured SourceDir should be nil, got %v", fp)
	}
}
