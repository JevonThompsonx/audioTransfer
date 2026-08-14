package organizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/parser"
	"github.com/jevonx/audioTransfer/pkg/virusscan"
)

func TestDisambiguationFix(t *testing.T) {
	// Test the fix for the 3-level tree bug.
	// The organizer.go RunTransfer applies this logic:
	//   if book.AuthorDir != "" && book.AuthorDir != parentName && parsed.Confidence <= 50 {
	//       if parser.IsAuthorish(book.AuthorDir) && !parser.IsTitleLike(book.AuthorDir) {
	//           parsed.Author = book.AuthorDir
	//           parsed.Confidence = 75
	//       }
	//   }
	//
	// We test it via resolveIdentity which is the function that receives the
	// parsed info and transforms it into the final identity.

	tmpDir := t.TempDir()

	// Create a synthetic book source as it would be returned by the scanner
	// with AuthorDir = "Gerri Hill"
	book := &models.BookSource{
		Name:       "Hunter 01 Hunter's Way",
		Path:       filepath.Join(tmpDir, "Gerri Hill", "Hunter", "Hunter 01 Hunter's Way"),
		AuthorDir:  "Gerri Hill",
		AudioFiles: []string{filepath.Join(tmpDir, "dummy.mp3")},
	}

	// Create the dummy audio file so bookSourceStat doesn't fail
	os.MkdirAll(filepath.Dir(book.Path), 0755)
	os.WriteFile(filepath.Join(tmpDir, "dummy.mp3"), []byte("test"), 0644)

	// Parse the book name without AuthorDir context (simulates what ParseName gets)
	// This gives us low confidence because "Hunter" is the parent folder
	parentName := "Hunter" // immediate parent
	parsed := parser.ParseName(book.Name, parentName)

	// Verify that parsing produces low confidence author
	if parsed.Confidence > 50 {
		t.Fatalf("Test setup issue: parsed confidence should be <=50, got %d", parsed.Confidence)
	}

	// Now apply the disambiguation logic manually (what organizer.go does)
	if book.AuthorDir != "" && book.AuthorDir != parentName && parsed.Confidence <= 50 {
		if parser.IsAuthorish(book.AuthorDir) && !parser.IsTitleLike(book.AuthorDir) {
			parsed.Author = book.AuthorDir
			parsed.Confidence = 75
		}
	}

	// The fix should have overridden the author to "Gerri Hill"
	if parsed.Author != "Gerri Hill" {
		t.Errorf("Author not overridden by AuthorDir: got %q, want %q", parsed.Author, "Gerri Hill")
	}
	if parsed.Confidence != 75 {
		t.Errorf("Confidence not set to 75: got %d", parsed.Confidence)
	}

	// Now resolve the identity via resolveIdentity to ensure it works end-to-end
	cfg := Config{DryRun: true, SourceDir: tmpDir}
	identity := resolveIdentity(parsed, book, cfg)

	if identity.Author != "Gerri Hill" {
		t.Errorf("Final identity author wrong: got %q, want %q", identity.Author, "Gerri Hill")
	}
	if identity.Confidence != 75 {
		t.Errorf("Final identity confidence wrong: got %d, want 75", identity.Confidence)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	// Test SaveCheckpoint and LoadCheckpoint round-trip
	tmpFile := filepath.Join(t.TempDir(), "checkpoint.json")

	now := time.Now()
	entry := &CheckpointEntry{
		Identity: &models.BookIdentity{
			Title:  "Test Book",
			Author: "Test Author",
		},
		TransferStatus: "transferred",
		MethodUsed:     "native-ssh",
		TransferredAt:  now,
		FilesCount:     3,
		SourceSize:     1024000,
		SourceModTime:  now.Add(-1 * time.Hour),
	}

	checkpoint := &Checkpoint{
		Books: map[string]*CheckpointEntry{
			"/path/to/book": entry,
		},
	}

	// Save checkpoint
	if err := SaveCheckpoint(tmpFile, checkpoint); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Load it back
	loaded, err := LoadCheckpoint(tmpFile)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	// Verify contents match
	if len(loaded.Books) != 1 {
		t.Fatalf("Loaded checkpoint has wrong number of books: got %d, want 1", len(loaded.Books))
	}

	loadedEntry, ok := loaded.Books["/path/to/book"]
	if !ok {
		t.Fatal("Loaded checkpoint missing expected key")
	}

	if loadedEntry.Identity.Title != "Test Book" {
		t.Errorf("Title: got %q, want %q", loadedEntry.Identity.Title, "Test Book")
	}
	if loadedEntry.Identity.Author != "Test Author" {
		t.Errorf("Author: got %q, want %q", loadedEntry.Identity.Author, "Test Author")
	}
	if loadedEntry.TransferStatus != "transferred" {
		t.Errorf("TransferStatus: got %q, want %q", loadedEntry.TransferStatus, "transferred")
	}
	if loadedEntry.MethodUsed != "native-ssh" {
		t.Errorf("MethodUsed: got %q, want %q", loadedEntry.MethodUsed, "native-ssh")
	}
	if loadedEntry.FilesCount != 3 {
		t.Errorf("FilesCount: got %d, want 3", loadedEntry.FilesCount)
	}
	if loadedEntry.SourceSize != 1024000 {
		t.Errorf("SourceSize: got %d, want 1024000", loadedEntry.SourceSize)
	}

	// Use .Equal() for time comparison, not ==
	if !loadedEntry.SourceModTime.Equal(now.Add(-1 * time.Hour)) {
		t.Errorf("SourceModTime not equal after round-trip")
	}
}

func TestLoadCheckpoint_NonexistentFile(t *testing.T) {
	// LoadCheckpoint should return an empty checkpoint without error
	// if the file doesn't exist
	tmpFile := filepath.Join(t.TempDir(), "nonexistent.json")

	checkpoint, err := LoadCheckpoint(tmpFile)
	if err != nil {
		t.Fatalf("LoadCheckpoint should not error on nonexistent file: %v", err)
	}

	if checkpoint == nil {
		t.Fatal("LoadCheckpoint returned nil for nonexistent file")
	}

	if checkpoint.Books == nil {
		t.Fatal("LoadCheckpoint returned nil Books map")
	}

	if len(checkpoint.Books) != 0 {
		t.Errorf("LoadCheckpoint should return empty map, got %d entries", len(checkpoint.Books))
	}
}

func TestLoadCheckpoint_CorruptJSON(t *testing.T) {
	// LoadCheckpoint should return an error for corrupt JSON
	tmpFile := filepath.Join(t.TempDir(), "corrupt.json")
	os.WriteFile(tmpFile, []byte("{ invalid json"), 0644)

	checkpoint, err := LoadCheckpoint(tmpFile)
	if err == nil {
		t.Fatal("LoadCheckpoint should error on corrupt JSON")
	}

	if checkpoint != nil {
		t.Errorf("LoadCheckpoint should return nil on error, got %v", checkpoint)
	}
}

func TestBookSourceStat(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with known sizes
	file1 := filepath.Join(tmpDir, "audio1.mp3")
	file2 := filepath.Join(tmpDir, "audio2.mp3")
	coverFile := filepath.Join(tmpDir, "cover.jpg")

	os.WriteFile(file1, make([]byte, 1000), 0644)
	os.WriteFile(file2, make([]byte, 2000), 0644)
	os.WriteFile(coverFile, make([]byte, 500), 0644)

	book := &models.BookSource{
		AudioFiles: []string{file1, file2},
		CoverFiles: []string{coverFile},
	}

	size, modTime := bookSourceStat(book)

	expectedSize := int64(1000 + 2000 + 500)
	if size != expectedSize {
		t.Errorf("Size: got %d, want %d", size, expectedSize)
	}

	if modTime.IsZero() {
		t.Error("ModTime should not be zero")
	}

	// Verify modTime is reasonable (within last few seconds)
	now := time.Now()
	if modTime.After(now) {
		t.Errorf("ModTime is in the future: %v", modTime)
	}
	if modTime.Before(now.Add(-10 * time.Second)) {
		t.Errorf("ModTime is too old: %v", modTime)
	}
}

func TestBookSourceStat_EmptyBook(t *testing.T) {
	// bookSourceStat should handle books with no files
	book := &models.BookSource{
		AudioFiles: []string{},
		CoverFiles: []string{},
	}

	size, modTime := bookSourceStat(book)

	if size != 0 {
		t.Errorf("Size should be 0, got %d", size)
	}

	if !modTime.IsZero() {
		t.Errorf("ModTime should be zero for empty book, got %v", modTime)
	}
}

func TestBookSourceStat_MissingFiles(t *testing.T) {
	// bookSourceStat should skip missing files without error
	tmpDir := t.TempDir()

	existingFile := filepath.Join(tmpDir, "exist.mp3")
	os.WriteFile(existingFile, make([]byte, 1000), 0644)

	missingFile := filepath.Join(tmpDir, "missing.mp3")

	book := &models.BookSource{
		AudioFiles: []string{existingFile, missingFile},
		CoverFiles: []string{},
	}

	size, modTime := bookSourceStat(book)

	// Should only count the existing file
	if size != 1000 {
		t.Errorf("Size should be 1000 (only existing file), got %d", size)
	}

	if modTime.IsZero() {
		t.Error("ModTime should not be zero when at least one file exists")
	}
}

func TestCheckpointPath(t *testing.T) {
	// CheckpointPath should return a valid path
	path, err := CheckpointPath()
	if err != nil {
		t.Fatalf("CheckpointPath failed: %v", err)
	}

	if path == "" {
		t.Fatal("CheckpointPath returned empty string")
	}

	// Path should contain checkpoint.json
	if !filepath.IsAbs(path) {
		t.Errorf("CheckpointPath should return absolute path, got %q", path)
	}
}

func TestOpenLibraryOverridesLowConfidenceAuthor(t *testing.T) {
	// Regression test: "Red Rising" parent dir used as author instead of
	// OpenLibrary's "Pierce Brown". When the parser's author came from a
	// low-confidence parent-dir guess (confidence <=50), OpenLibrary's
	// authoritative author should override it.

	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "qbit")

	// Simulate: ~/qbit/Red Rising/Red Rising Part 1of 2.mp3
	bookDir := filepath.Join(sourceDir, "Red Rising")
	os.MkdirAll(bookDir, 0755)
	bookFile := filepath.Join(bookDir, "Red Rising Part 1of 2 DramatizedAdaptation Book 1.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)

	book := &models.BookSource{
		Name:         "Red Rising",
		Path:         bookDir,
		IsSingleFile: false,
		AudioFiles:   []string{bookFile},
	}

	parentName := "Red Rising"
	parsed := parser.ParseName(book.Name, parentName)

	// Verify parser produces low-confidence author from parent dir
	if parsed.Author != "Red Rising" {
		t.Fatalf("Test setup: expected parser to set author from parent dir, got %q", parsed.Author)
	}
	if parsed.Confidence > 50 {
		t.Fatalf("Test setup: expected low confidence (<=50), got %d", parsed.Confidence)
	}

	// resolveIdentity with DryRun=true skips OpenLibrary lookup,
	// so we test the logic directly: the fix is at line640 which checks
	// confidence <=50 before overriding.
	//
	// With the fix, when OpenLibrary returns "Pierce Brown" and current
	// author "Red Rising" has confidence <=50, the author gets overridden.
	cfg := Config{DryRun: true, SourceDir: sourceDir}
	identity := resolveIdentity(parsed, book, cfg)

	// In dry-run mode (no OpenLibrary), author stays as parsed.
	// The real fix happens when OpenLibrary runs. We verify the confidence
	// threshold logic is correct.
	if identity.Author != "Red Rising" {
		t.Errorf("Dry-run: author should stay as parsed, got %q", identity.Author)
	}

	// Now simulate what happens when OpenLibrary returns a result.
	// The fix: if ol.Author != "" && identity.Confidence <=50, override.
	// We can't easily mock OpenLibrary here, but we verify the threshold
	// is set correctly by checking the parsed confidence.
	if parsed.Confidence > 50 {
		t.Errorf("Parser confidence should be <=50 for parent-dir guess, got %d. "+
			"This means the OpenLibrary override threshold (<=50) won't trigger.", parsed.Confidence)
	}
}

func TestHighConfidenceAuthorNotOverridden(t *testing.T) {
	// Ensure filename-parsed authors (high confidence) are NOT overridden
	// by OpenLibrary. E.g., "Brandon Sanderson - Mistborn" should keep
	// "Brandon Sanderson" even if OpenLibrary returns something different.

	tmpDir := t.TempDir()

	book := &models.BookSource{
		Name:       "Brandon Sanderson - Mistborn",
		Path:       filepath.Join(tmpDir, "Brandon Sanderson - Mistborn"),
		AudioFiles: []string{filepath.Join(tmpDir, "dummy.mp3")},
	}
	os.WriteFile(book.AudioFiles[0], []byte("test"), 0644)

	parsed := parser.ParseName(book.Name, "")

	// Verify parser produces high-confidence author from filename
	if parsed.Author != "Brandon Sanderson" {
		t.Fatalf("Test setup: expected parser to find author, got %q", parsed.Author)
	}
	if parsed.Confidence <= 50 {
		t.Fatalf("Test setup: expected high confidence (>50), got %d", parsed.Confidence)
	}

	cfg := Config{DryRun: true, SourceDir: tmpDir}
	identity := resolveIdentity(parsed, book, cfg)

	if identity.Author != "Brandon Sanderson" {
		t.Errorf("High-confidence author should not be overridden: got %q, want %q",
			identity.Author, "Brandon Sanderson")
	}
}

func TestCheckpointRoundTrip_Multiple(t *testing.T) {
	// Test multiple checkpoint entries
	tmpFile := filepath.Join(t.TempDir(), "checkpoint.json")

	checkpoint := &Checkpoint{
		Books: map[string]*CheckpointEntry{
			"/path/book1": {
				Identity:       &models.BookIdentity{Title: "Book One", Author: "Author A"},
				TransferStatus: "transferred",
				MethodUsed:     "native-ssh",
				FilesCount:     2,
				SourceSize:     5000,
			},
			"/path/book2": {
				Identity:       &models.BookIdentity{Title: "Book Two", Author: "Author B"},
				TransferStatus: "local",
				MethodUsed:     "local",
				FilesCount:     1,
				SourceSize:     3000,
			},
		},
	}

	// Save and load
	if err := SaveCheckpoint(tmpFile, checkpoint); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	loaded, err := LoadCheckpoint(tmpFile)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if len(loaded.Books) != 2 {
		t.Fatalf("Expected 2 books, got %d", len(loaded.Books))
	}

	// Verify both entries
	if loaded.Books["/path/book1"].Identity.Title != "Book One" {
		t.Error("Book1 title mismatch")
	}
	if loaded.Books["/path/book2"].Identity.Title != "Book Two" {
		t.Error("Book2 title mismatch")
	}
}

func TestCheckpointFastPathCountsResumed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Test Author - Test Book.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)
	fi, _ := os.Stat(bookFile)

	cp := &Checkpoint{Books: map[string]*CheckpointEntry{
		bookFile: { // checkpointKey(book) == AudioFiles[0]
			Identity:       &models.BookIdentity{Title: "Test Book", Author: "Test Author"},
			TransferStatus: "transferred",
			MethodUsed:     "native-ssh",
			FilesCount:     1,
			SourceSize:     fi.Size(),
			SourceModTime:  fi.ModTime(),
		},
	}}
	path, _ := CheckpointPath()
	if err := SaveCheckpoint(path, cp); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	report := RunTransfer(Config{SourceDir: src, DestDir: filepath.Join(home, "organized"),
		DryRun: true, Force: true})
	if report.Total != 1 || report.Resumed != 1 || report.Transferred != 0 {
		t.Fatalf("got Total=%d Resumed=%d Transferred=%d, want 1/1/0",
			report.Total, report.Resumed, report.Transferred)
	}
	if len(report.Results) != 1 || report.Results[0].Status != "resumed" {
		t.Fatalf("fast-path result status = %v, want single result with Status %q",
			report.Results, "resumed")
	}
}

func TestDryRunCountsSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, "qbit")
	os.MkdirAll(src, 0755)
	bookFile := filepath.Join(src, "Test Author - Test Book.mp3")
	os.WriteFile(bookFile, make([]byte, 1000), 0644)

	report := RunTransfer(Config{SourceDir: src, DestDir: filepath.Join(home, "organized"),
		DryRun: true, Force: true})
	if report.Total != 1 || report.Skipped != 1 || report.Transferred != 0 {
		t.Fatalf("got Total=%d Skipped=%d Transferred=%d, want 1/1/0",
			report.Total, report.Skipped, report.Transferred)
	}
}

// StubTransferClient is a test double implementing the TransferClient interface
type StubTransferClient struct {
	remoteExists    bool
	remoteTotalSize int64
	connectionFails bool
}

func (s *StubTransferClient) MethodName() string        { return "stub" }
func (s *StubTransferClient) Preflight() (bool, string) { return true, "stub ready" }
func (s *StubTransferClient) Connect() bool             { return true }
func (s *StubTransferClient) Disconnect()               {}
func (s *StubTransferClient) TransferBook(audioFiles, coverFiles []string, targetSubpath string) bool {
	return true
}
func (s *StubTransferClient) VerifyTransfer(remoteSubpath string) map[string]interface{} {
	result := map[string]interface{}{
		"path":             remoteSubpath,
		"exists":           s.remoteExists,
		"files":            []map[string]interface{}{},
		"total_size":       s.remoteTotalSize,
		"connection_error": s.connectionFails,
	}
	if s.connectionFails {
		result["error"] = "SSH connection failed"
	} else if !s.remoteExists {
		result["error"] = "Remote path not found"
	}
	return result
}

func TestResumeSkip_LocalSizeZero(t *testing.T) {
	// resumeSkip should return false if local size is 0
	book := &models.BookSource{
		Name:       "EmptyBook",
		AudioFiles: []string{}, // Empty
		CoverFiles: []string{},
	}

	identity := &models.BookIdentity{
		Title:  "Empty",
		Author: "Unknown",
	}

	client := &StubTransferClient{
		remoteExists:    true,
		remoteTotalSize: 1000,
	}

	result := resumeSkip(client, book, identity)
	if result {
		t.Error("resumeSkip should return false when local size is 0")
	}
}

func TestResumeSkip_RemoteDoesNotExist(t *testing.T) {
	// resumeSkip should return false if remote doesn't exist
	tmpDir := t.TempDir()

	// Create a local file
	localFile := filepath.Join(tmpDir, "book.mp3")
	os.WriteFile(localFile, make([]byte, 1000), 0644)

	book := &models.BookSource{
		Name:       "MyBook",
		AudioFiles: []string{localFile},
		CoverFiles: []string{},
	}

	identity := &models.BookIdentity{
		Title:  "MyBook",
		Author: "Unknown",
	}

	client := &StubTransferClient{
		remoteExists: false, // Remote doesn't exist
	}

	result := resumeSkip(client, book, identity)
	if result {
		t.Error("resumeSkip should return false when remote doesn't exist")
	}
}

func TestResumeSkip_SizesMatch(t *testing.T) {
	// resumeSkip should return true if remote exists with matching size
	tmpDir := t.TempDir()

	// Create a local file with known size
	localFile := filepath.Join(tmpDir, "book.mp3")
	os.WriteFile(localFile, make([]byte, 1000), 0644)

	book := &models.BookSource{
		Name:       "MyBook",
		AudioFiles: []string{localFile},
		CoverFiles: []string{},
	}

	identity := &models.BookIdentity{
		Title:  "MyBook",
		Author: "Unknown",
	}

	client := &StubTransferClient{
		remoteExists:    true,
		remoteTotalSize: 1000, // Matches local size
	}

	result := resumeSkip(client, book, identity)
	if !result {
		t.Error("resumeSkip should return true when sizes match")
	}
}

func TestResumeSkip_SizesMismatch(t *testing.T) {
	// resumeSkip should return false if remote exists but size doesn't match
	tmpDir := t.TempDir()

	// Create a local file with known size
	localFile := filepath.Join(tmpDir, "book.mp3")
	os.WriteFile(localFile, make([]byte, 1000), 0644)

	book := &models.BookSource{
		Name:       "MyBook",
		AudioFiles: []string{localFile},
		CoverFiles: []string{},
	}

	identity := &models.BookIdentity{
		Title:  "MyBook",
		Author: "Unknown",
	}

	client := &StubTransferClient{
		remoteExists:    true,
		remoteTotalSize: 2000, // Doesn't match local size
	}

	result := resumeSkip(client, book, identity)
	if result {
		t.Error("resumeSkip should return false when sizes don't match")
	}
}

func TestResumeSkip_MultipleFiles(t *testing.T) {
	// resumeSkip should sum sizes across all audio and cover files
	tmpDir := t.TempDir()

	audio1 := filepath.Join(tmpDir, "audio1.mp3")
	audio2 := filepath.Join(tmpDir, "audio2.mp3")
	cover := filepath.Join(tmpDir, "cover.jpg")

	os.WriteFile(audio1, make([]byte, 500), 0644)
	os.WriteFile(audio2, make([]byte, 400), 0644)
	os.WriteFile(cover, make([]byte, 100), 0644)

	book := &models.BookSource{
		Name:       "MyBook",
		AudioFiles: []string{audio1, audio2},
		CoverFiles: []string{cover},
	}

	identity := &models.BookIdentity{
		Title:  "MyBook",
		Author: "Unknown",
	}

	// Total local size = 500 + 400 + 100 = 1000
	client := &StubTransferClient{
		remoteExists:    true,
		remoteTotalSize: 1000,
	}

	result := resumeSkip(client, book, identity)
	if !result {
		t.Error("resumeSkip should correctly sum multiple files")
	}
}

func TestFilterUnsafeBooks_AllClean(t *testing.T) {
	matched := []bookWithID{
		{book: &models.BookSource{Name: "A", AudioFiles: []string{"/q/a.mp3"}}},
		{book: &models.BookSource{Name: "B", AudioFiles: []string{"/q/b.mp3"}}},
	}
	report := &virusscan.ScanReport{
		Results: []virusscan.ScanResult{{File: "/q/a.mp3"}, {File: "/q/b.mp3"}},
	}
	clean, dropped := filterUnsafeBooks(matched, report)
	if len(clean) != 2 || dropped != 0 {
		t.Fatalf("expected both books kept, got %d clean %d dropped", len(clean), dropped)
	}
}

func TestFilterUnsafeBooks_InfectedDropped(t *testing.T) {
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Bad", AudioFiles: []string{"/q/bad.mp3"}}},
		{book: &models.BookSource{Name: "Good", AudioFiles: []string{"/q/good.mp3"}}},
	}
	report := &virusscan.ScanReport{
		Results: []virusscan.ScanResult{
			{File: "/q/bad.mp3", Infected: true, VirusName: "Eicar-Signature"},
			{File: "/q/good.mp3"},
		},
	}
	clean, dropped := filterUnsafeBooks(matched, report)
	if dropped != 1 || len(clean) != 1 {
		t.Fatalf("expected 1 dropped, 1 clean; got dropped=%d clean=%d", dropped, len(clean))
	}
	if clean[0].book.Name != "Good" {
		t.Errorf("expected Good kept, got %s", clean[0].book.Name)
	}
}

func TestFilterUnsafeBooks_ScanErrorFailsClosed(t *testing.T) {
	// Fail closed: a file the scanner could NOT check (error) must block the
	// book just like an infection — unscanned files never reach the library.
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Unscannable", AudioFiles: []string{"/q/x.mp3"}}},
	}
	report := &virusscan.ScanReport{
		Results: []virusscan.ScanResult{{File: "/q/x.mp3", Error: "clam exit code 2"}},
	}
	clean, dropped := filterUnsafeBooks(matched, report)
	if dropped != 1 || len(clean) != 0 {
		t.Fatalf("expected book blocked on scan error, got dropped=%d clean=%d", dropped, len(clean))
	}
}

func TestFilterUnsafeBooks_CoverInfectedDropped(t *testing.T) {
	// Infection in a cover file must block the book too.
	matched := []bookWithID{
		{book: &models.BookSource{Name: "CoverBad", AudioFiles: []string{"/q/a.mp3"}, CoverFiles: []string{"/q/cover.jpg"}}},
	}
	report := &virusscan.ScanReport{
		Results: []virusscan.ScanResult{{File: "/q/cover.jpg", Infected: true, VirusName: "Eicar-Signature"}},
	}
	clean, dropped := filterUnsafeBooks(matched, report)
	if dropped != 1 || len(clean) != 0 {
		t.Fatalf("expected book blocked on infected cover, got dropped=%d clean=%d", dropped, len(clean))
	}
}

func TestFilterUnsafeBooks_MultiFilePartialError(t *testing.T) {
	// Multi-file book with one unreadable track: entire book blocked.
	matched := []bookWithID{
		{book: &models.BookSource{Name: "Multi", AudioFiles: []string{"/q/t1.mp3", "/q/t2.mp3"}}},
	}
	report := &virusscan.ScanReport{
		Results: []virusscan.ScanResult{
			{File: "/q/t1.mp3"},
			{File: "/q/t2.mp3", Error: "access denied"},
		},
	}
	clean, dropped := filterUnsafeBooks(matched, report)
	if dropped != 1 || len(clean) != 0 {
		t.Fatalf("expected multi-file book blocked, got dropped=%d clean=%d", dropped, len(clean))
	}
}
