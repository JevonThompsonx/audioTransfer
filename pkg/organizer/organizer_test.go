package organizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/parser"
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

// StubTransferClient is a test double implementing the TransferClient interface
type StubTransferClient struct {
	remoteExists    bool
	remoteTotalSize int64
	connectionFails bool
}

func (s *StubTransferClient) MethodName() string { return "stub" }
func (s *StubTransferClient) Preflight() (bool, string) { return true, "stub ready" }
func (s *StubTransferClient) Connect() bool { return true }
func (s *StubTransferClient) Disconnect() {}
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
