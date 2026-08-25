// Package scanner provides directory scanning for audiobook files.
// Supports recursive discovery, zip extraction, and smart grouping.
package scanner

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/parser"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

// ScanDirConfiguration configures the directory scanner.
type ScanDirConfiguration struct {
	SourceDir   string
	ExtractZips bool
}

// ScanDirectory scans a directory for audiobook files and returns grouped sources.
// Handles three levels of nesting:
//  1. Top-level .m4b/.mp3 files → each is a book
//  2. Top-level directories with audio → each is a book
//  3. Top-level directories containing subdirectories with audio → container with nested books
func ScanDirectory(cfg ScanDirConfiguration) []*models.BookSource {
	info, err := os.Stat(cfg.SourceDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	utils.Info.Printf("Scanning: %s", cfg.SourceDir)
	var books []*models.BookSource

	// Handle zip files first
	if cfg.ExtractZips {
		zips, _ := filepath.Glob(filepath.Join(cfg.SourceDir, "*.zip"))
		sort.Strings(zips)
		for _, zipPath := range zips {
			extracted := extractAndScanZip(zipPath)
			books = append(books, extracted...)
		}
	}

	// Read top-level entries
	entries, err := os.ReadDir(cfg.SourceDir)
	if err != nil {
		utils.Error.Printf("Cannot read source directory: %v", err)
		return books
	}

	// Skip known non-book directories
	skipNames := map[string]bool{
		"organized": true, "organize-audiobooks": true,
		"temp": true, "audiobook_transfer": true,
		"go-audiobook-transfer": true,
	}

	for _, entry := range entries {
		name := entry.Name()
		if skipNames[name] || strings.HasPrefix(name, ".") {
			continue
		}

		abs := filepath.Join(cfg.SourceDir, name)

		if entry.IsDir() {
			scanDirEntry(abs, name, name, &books)
		} else {
			ext := filepath.Ext(name)
			if utils.IsAudio(abs) {
				book := &models.BookSource{
					Name:         strings.TrimSuffix(name, ext),
					Path:         cfg.SourceDir,
					AudioFiles:   []string{abs},
					IsSingleFile: true,
				}
				books = append(books, book)
			}
		}
	}

	return books
}

// walkState accumulates everything scanDirEntry needs to classify a directory
// in a single recursive walk, replacing the previous pattern of one full WalkDir
// to collect audio followed by two more full passes (hasAudioInDir +
// hasAnySubBookDir) per directory — which re-walked the same tree repeatedly.
type walkState struct {
	audioFiles   []string
	coverFiles   []string
	hasDirect    bool // audio directly in this directory (not a subdir)
	hasSubBook   bool // some subdirectory holds audio
}

// collectWalkState performs a single recursive walk that gathers audio/cover
// files and classifies the directory as flat or a container of sub-books.
func collectWalkState(abs string) walkState {
	var st walkState
	_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if utils.IsAudio(p) {
			st.audioFiles = append(st.audioFiles, p)
			// Direct audio = a file whose parent is the scanned root itself.
			if filepath.Dir(p) == abs {
				st.hasDirect = true
			} else {
				st.hasSubBook = true
			}
		} else if utils.IsCover(p) {
			st.coverFiles = append(st.coverFiles, p)
		}
		return nil
	})
	return st
}

// scanDirEntry categorizes a directory entry as a book dir, series dir, or container.
func scanDirEntry(abs, name, authorDir string, books *[]*models.BookSource) {
	// Detect "Series Name (Author)" pattern — treat as a series container
	if isSeriesDir(name) {
		subBooks := scanContainerDir(abs, authorDir)
		utils.Debug.Printf("  Series dir: %s (%d sub-books)", name, len(subBooks))
		*books = append(*books, subBooks...)
		return
	}

	// Single recursive walk gathers audio/covers and classifies the directory.
	st := collectWalkState(abs)

	if len(st.audioFiles) == 0 {
		return
	}

	sort.Strings(st.audioFiles)
	sort.Strings(st.coverFiles)

	// Determine if this is a flat book dir or a container with sub-books.
	if (st.hasSubBook && !st.hasDirect) || (st.hasSubBook && len(st.audioFiles) > 3) {
		// Container: recurse into subdirectories
		subBooks := scanContainerDir(abs, authorDir)
		*books = append(*books, subBooks...)
	} else {
		// Flat book dir or series dir with direct audio.
		// Prefer the richer of (dir name, audio file stem) as the book name:
		// a single "Book.mp3" stem often carries the full title/author/ASIN the
		// dir name lacks, but the inverse also happens ("[M4B] Andy Weir -
		// Project Hail Mary/" containing "Project Hail Mary.m4b") — pick the
		// name that parses with higher confidence.
		bookName := name
		if st.hasDirect && len(st.audioFiles) == 1 {
			fileStem := strings.TrimSuffix(filepath.Base(st.audioFiles[0]), filepath.Ext(st.audioFiles[0]))
			dirInfo := parser.ParseName(name, "")
			fileInfo := parser.ParseName(fileStem, "")
			if fileInfo.Confidence > dirInfo.Confidence {
				bookName = fileStem
			}
		}
		book := &models.BookSource{
			Name:       bookName,
			Path:       abs,
			AudioFiles: st.audioFiles,
			CoverFiles: st.coverFiles,
			AuthorDir:  authorDir,
		}
		*books = append(*books, book)
		utils.Debug.Printf("  Book dir: %s (%d audio, %d covers)", name, len(st.audioFiles), len(st.coverFiles))
	}
}

// isSeriesDir checks if a directory name follows "Series Name (Author)" pattern.
// Example: "Realm of the Elderlings (Robin Hobb)", "Dragonriders of Pern (Anne McCaffrey)"
func isSeriesDir(name string) bool {
	lastOpen := strings.LastIndex(name, "(")
	if lastOpen < 0 {
		return false
	}
	lastClose := strings.LastIndex(name, ")")
	if lastClose <= lastOpen {
		return false
	}
	parenContent := strings.TrimSpace(name[lastOpen+1 : lastClose])
	if parenContent == "" {
		return false
	}
	// Parens content should look like an author name (short, 1-4 words)
	words := strings.Fields(parenContent)
	if len(words) < 1 || len(words) > 4 {
		return false
	}
	// The "before" part should NOT contain " - " (that's a regular book dir)
	before := strings.TrimSpace(name[:lastOpen])
	if strings.Contains(before, " - ") {
		return false
	}
	return true
}

// hasAnyAudio checks if a directory tree contains any audio files.
func hasAnyAudio(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			if found {
				return filepath.SkipAll
			}
			return nil
		}
		if !d.IsDir() && utils.IsAudio(p) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// scanContainerDir handles directories that contain multiple separate sub-books.
// This includes both "Series (Author)" directories and flat containers with "Book - Author" subdirectories.
func scanContainerDir(dir string, authorDir string) []*models.BookSource {
	var books []*models.BookSource
	entries, err := os.ReadDir(dir)
	if err != nil {
		return books
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		abs := filepath.Join(dir, name)

		if e.IsDir() && hasAnyAudio(abs) {
			// Recurse using the same book-vs-container detection as top-level
			// entries, so a container whose children are themselves containers
			// (e.g. Author/Series/Title/files) splits into per-title books
			// instead of collapsing into one flat book per child.
			scanDirEntry(abs, name, authorDir, &books)
		} else if !e.IsDir() {
			ext := filepath.Ext(name)
			if utils.IsAudio(abs) {
				book := &models.BookSource{
					Name:         strings.TrimSuffix(name, ext),
					Path:         dir,
					AudioFiles:   []string{abs},
					IsSingleFile: true,
					AuthorDir:    authorDir,
				}
				books = append(books, book)
			} else if utils.IsCover(abs) || utils.IsMeta(abs) {
				// Collect loose cover/metadata files at container level
				// These will be picked up by the organizer when processing sub-books
			}
		}
	}

	return books
}

// extractAndScanZip extracts a zip file to temp dir and scans its contents.
// Includes zip-slip protection.
func extractAndScanZip(zipPath string) []*models.BookSource {
	utils.Info.Printf("Extracting zip: %s", filepath.Base(zipPath))
	var books []*models.BookSource

	stem := strings.TrimSuffix(filepath.Base(zipPath), ".zip")
	tmpDir, cleanup, err := utils.TempDir(fmt.Sprintf("zip_%s_", stem))
	if err != nil {
		utils.Error.Printf("Failed to create temp dir for zip: %v", err)
		return books
	}
	defer cleanup()

	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		utils.Error.Printf("Corrupted zip file: %s", zipPath)
		return books
	}
	defer reader.Close()

	for _, f := range reader.File {
		path := filepath.Join(tmpDir, f.Name)

		// Path traversal protection
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		dst, err := os.Create(path)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(dst, rc)
		rc.Close()
		dst.Close()
	}

	extracted := ScanDirectory(ScanDirConfiguration{SourceDir: tmpDir, ExtractZips: false})
	for _, b := range extracted {
		b.IsFromZip = true
		b.ZipPath = zipPath
		if b.Name != stem {
			b.Name = stem + "/" + b.Name
		}
	}
	books = append(books, extracted...)

	return books
}
