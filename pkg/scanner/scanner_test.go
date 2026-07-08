package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirectory_TwoLevelTree(t *testing.T) {
	// Create a 2-level tree: Author/Title/files
	tmpDir := t.TempDir()
	authorDir := filepath.Join(tmpDir, "Gerri Hill")
	bookDir := filepath.Join(authorDir, "Mine")
	os.MkdirAll(bookDir, 0755)

	// Create audio files
	audioFile := filepath.Join(bookDir, "Mine.mp3")
	os.WriteFile(audioFile, []byte{}, 0644)

	// Create cover file
	coverFile := filepath.Join(bookDir, "cover.jpg")
	os.WriteFile(coverFile, []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 1 {
		t.Fatalf("Expected 1 book, got %d", len(books))
	}

	book := books[0]
	if book.AuthorDir != "Gerri Hill" {
		t.Errorf("AuthorDir: got %q, want %q", book.AuthorDir, "Gerri Hill")
	}
	if book.Name != "Mine" {
		t.Errorf("Name: got %q, want %q", book.Name, "Mine")
	}
	if len(book.AudioFiles) != 1 {
		t.Errorf("AudioFiles: got %d, want 1", len(book.AudioFiles))
	}
	if len(book.CoverFiles) != 1 {
		t.Errorf("CoverFiles: got %d, want 1", len(book.CoverFiles))
	}
}

func TestScanDirectory_ThreeLevelBugFix(t *testing.T) {
	// This test reproduces the real bug from tonight:
	// Gerri Hill/Hunter/Hunter 01 Hunter's Way/file.mp3
	// and
	// Gerri Hill/Hunter/Hunter 02 Aftermath/file.mp3
	//
	// Both should have AuthorDir == "Gerri Hill", not "Hunter"
	// (Hunter is an intermediate series folder, not the author).

	tmpDir := t.TempDir()

	// Create the 3-level structure
	// Gerri Hill (author) / Hunter (series) / Hunter 01 Hunter's Way / files
	authorDir := filepath.Join(tmpDir, "Gerri Hill")
	seriesDir := filepath.Join(authorDir, "Hunter")
	book1Dir := filepath.Join(seriesDir, "Hunter 01 Hunter's Way")
	book2Dir := filepath.Join(seriesDir, "Hunter 02 Aftermath")

	os.MkdirAll(book1Dir, 0755)
	os.MkdirAll(book2Dir, 0755)

	// Create audio files
	os.WriteFile(filepath.Join(book1Dir, "Hunter_01_Hunter's_Way.m4b"), []byte{}, 0644)
	os.WriteFile(filepath.Join(book2Dir, "Hunter_02_Aftermath.m4b"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 2 {
		t.Fatalf("Expected 2 books, got %d", len(books))
	}

	// Check both books have the correct AuthorDir
	for i, book := range books {
		if book.AuthorDir != "Gerri Hill" {
			t.Errorf("Book %d AuthorDir: got %q, want %q", i, book.AuthorDir, "Gerri Hill")
		}
	}
}

func TestScanDirectory_FlatSingleFile(t *testing.T) {
	// Test a flat single-file book at the source root (no author folder)
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "StandaloneBook.mp3"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 1 {
		t.Fatalf("Expected 1 book, got %d", len(books))
	}

	book := books[0]
	if book.AuthorDir != "" {
		t.Errorf("AuthorDir: got %q, want %q (empty)", book.AuthorDir, "")
	}
	if book.Name != "StandaloneBook" {
		t.Errorf("Name: got %q, want %q", book.Name, "StandaloneBook")
	}
	if !book.IsSingleFile {
		t.Error("IsSingleFile: expected true")
	}
}

func TestScanDirectory_ContainerMultipleBooks(t *testing.T) {
	// Multiple flat sub-books directly under one author
	// Author / Book1 / file.mp3
	// Author / Book2 / file.mp3
	tmpDir := t.TempDir()

	authorDir := filepath.Join(tmpDir, "John Smith")
	book1Dir := filepath.Join(authorDir, "Book One")
	book2Dir := filepath.Join(authorDir, "Book Two")

	os.MkdirAll(book1Dir, 0755)
	os.MkdirAll(book2Dir, 0755)

	os.WriteFile(filepath.Join(book1Dir, "Book_One.mp3"), []byte{}, 0644)
	os.WriteFile(filepath.Join(book2Dir, "Book_Two.mp3"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 2 {
		t.Fatalf("Expected 2 books, got %d", len(books))
	}

	// Both should have AuthorDir == "John Smith"
	for i, book := range books {
		if book.AuthorDir != "John Smith" {
			t.Errorf("Book %d AuthorDir: got %q, want %q", i, book.AuthorDir, "John Smith")
		}
	}
}

func TestScanDirectory_SeriesPattern(t *testing.T) {
	// Test "Series Name (Author)" pattern — should be detected as a container
	tmpDir := t.TempDir()

	// Create "Mercy Thompson (Patricia Briggs)" directory
	seriesDir := filepath.Join(tmpDir, "Patricia Briggs")
	containerDir := filepath.Join(seriesDir, "Mercy Thompson (Patricia Briggs)")
	book1Dir := filepath.Join(containerDir, "Mercy Thompson 01 - Moon Called")
	book2Dir := filepath.Join(containerDir, "Mercy Thompson 02 - Blood Bound")

	os.MkdirAll(book1Dir, 0755)
	os.MkdirAll(book2Dir, 0755)

	os.WriteFile(filepath.Join(book1Dir, "Moon_Called.m4b"), []byte{}, 0644)
	os.WriteFile(filepath.Join(book2Dir, "Blood_Bound.m4b"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 2 {
		t.Fatalf("Expected 2 books, got %d", len(books))
	}

	// Both should have AuthorDir == "Patricia Briggs"
	for i, book := range books {
		if book.AuthorDir != "Patricia Briggs" {
			t.Errorf("Book %d AuthorDir: got %q, want %q", i, book.AuthorDir, "Patricia Briggs")
		}
	}
}

func TestScanDirectory_IgnoresKnownDirs(t *testing.T) {
	// Test that known non-book directories are skipped
	tmpDir := t.TempDir()

	// Create some directories that should be skipped
	os.MkdirAll(filepath.Join(tmpDir, "organized"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "organized", "file.mp3"), []byte{}, 0644)

	os.MkdirAll(filepath.Join(tmpDir, "temp"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "temp", "file.mp3"), []byte{}, 0644)

	// Create a valid book directory
	validDir := filepath.Join(tmpDir, "MyBook")
	os.MkdirAll(validDir, 0755)
	os.WriteFile(filepath.Join(validDir, "book.mp3"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	// Should only find the valid book, not the ones in organized/temp
	if len(books) != 1 {
		t.Fatalf("Expected 1 book, got %d", len(books))
	}
}

func TestScanDirectory_HiddenFilesIgnored(t *testing.T) {
	// Test that hidden files/directories are ignored
	tmpDir := t.TempDir()

	// Create a hidden directory
	os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".hidden", "file.mp3"), []byte{}, 0644)

	// Create a valid book directory
	validDir := filepath.Join(tmpDir, "MyBook")
	os.MkdirAll(validDir, 0755)
	os.WriteFile(filepath.Join(validDir, "book.mp3"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 1 {
		t.Fatalf("Expected 1 book, got %d", len(books))
	}
}

func TestScanDirectory_EmptyDirectory(t *testing.T) {
	// Test scanning an empty directory
	tmpDir := t.TempDir()

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 0 {
		t.Fatalf("Expected 0 books, got %d", len(books))
	}
}

func TestScanDirectory_NonexistentDirectory(t *testing.T) {
	// Test scanning a non-existent directory
	cfg := ScanDirConfiguration{SourceDir: "/nonexistent/path", ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 0 {
		t.Fatalf("Expected 0 books for non-existent dir, got %d", len(books))
	}
}

func TestScanDirectory_MultipleAudioFormats(t *testing.T) {
	// Test that various audio formats are detected
	tmpDir := t.TempDir()

	bookDir := filepath.Join(tmpDir, "MyBook")
	os.MkdirAll(bookDir, 0755)

	// Create various audio files
	os.WriteFile(filepath.Join(bookDir, "part1.mp3"), []byte{}, 0644)
	os.WriteFile(filepath.Join(bookDir, "part2.m4b"), []byte{}, 0644)
	os.WriteFile(filepath.Join(bookDir, "part3.m4a"), []byte{}, 0644)

	cfg := ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false}
	books := ScanDirectory(cfg)

	if len(books) != 1 {
		t.Fatalf("Expected 1 book, got %d", len(books))
	}

	if len(books[0].AudioFiles) != 3 {
		t.Errorf("Expected 3 audio files, got %d", len(books[0].AudioFiles))
	}
}
