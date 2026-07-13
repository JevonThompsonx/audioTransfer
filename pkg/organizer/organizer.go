// Package organizer orchestrates the full audiobook scan→parse→match→transfer pipeline.
package organizer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jevonx/audioTransfer/pkg/metadata"
	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/parser"
	"github.com/jevonx/audioTransfer/pkg/scanner"
	"github.com/jevonx/audioTransfer/pkg/transfer"
	"github.com/jevonx/audioTransfer/pkg/utils"
)



// Config holds the pipeline configuration.
type Config struct {
	SourceDir   string
	DestDir     string
	Host        string
	TargetBase  string
	SSHKeyPath  string
	DryRun      bool
	Verbose     bool
	Force       bool
	Interactive bool
	Verify      bool
	LocalOnly   bool
	Methods     []string
	Parallel    int
}

// CheckpointEntry holds the checkpoint state for a single book.
type CheckpointEntry struct {
	Identity       *models.BookIdentity
	TransferStatus string    // "transferred" or "local"
	MethodUsed     string
	TransferredAt  time.Time
	FilesCount     int
	SourceSize     int64     // sum of book's audio+cover file sizes at checkpoint time
	SourceModTime  time.Time // latest mtime among the book's source files at checkpoint time
}

// Checkpoint holds the checkpoint state for all books processed.
type Checkpoint struct {
	Books map[string]*CheckpointEntry // keyed by book.Path (absolute source path)
}

// CheckpointPath returns the path to the checkpoint file in the config directory.
func CheckpointPath() (string, error) {
	dir, err := utils.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "checkpoint.json"), nil
}

// LoadCheckpoint loads the checkpoint from disk. If the file doesn't exist,
// returns an empty checkpoint with no error.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Checkpoint{Books: make(map[string]*CheckpointEntry)}, nil
		}
		return nil, err
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	if cp.Books == nil {
		cp.Books = make(map[string]*CheckpointEntry)
	}
	return &cp, nil
}

// SaveCheckpoint saves the checkpoint to disk atomically by writing to a temp
// file first and then renaming.
func SaveCheckpoint(path string, cp *Checkpoint) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// bookSourceStat computes the total size and latest modification time of a book's
// source files (audio + cover files).
func bookSourceStat(book *models.BookSource) (size int64, modTime time.Time) {
	for _, f := range append(append([]string{}, book.AudioFiles...), book.CoverFiles...) {
		if fi, err := os.Stat(f); err == nil {
			size += fi.Size()
			if fi.ModTime().After(modTime) {
				modTime = fi.ModTime()
			}
		}
	}
	return
}

// checkpointKey returns a stable, unique key for a book to use in the
// checkpoint map. book.Path is NOT safe for this: for a standalone top-level
// audio file (BookSource.IsSingleFile), the scanner deliberately sets Path to
// the file's *containing directory* (not the file itself), so that
// filepath.Base(book.Path) yields useful parent-directory context elsewhere
// in this file. That means two different top-level single-file downloads in
// the same source directory would collide on an identical book.Path and
// silently overwrite each other's checkpoint entry. book.AudioFiles[0] is
// always the book's own actual file path and is safe to use as a unique key.
func checkpointKey(book *models.BookSource) string {
	if len(book.AudioFiles) > 0 {
		return book.AudioFiles[0]
	}
	return book.Path
}

// RunTransfer executes the full audiobook transfer pipeline.
func RunTransfer(cfg Config) *models.TransferReport {
	report := &models.TransferReport{}

	fmt.Printf("\n[1/4] Scanning %s...\n", cfg.SourceDir)

	books := scanner.ScanDirectory(scanner.ScanDirConfiguration{
		SourceDir:   cfg.SourceDir,
		ExtractZips: true,
	})
	report.Total = len(books)

	if len(books) == 0 {
		fmt.Println("No audiobooks found.")
		return report
	}

	// Load checkpoint
	var checkpoint *Checkpoint
	checkpointPath, err := CheckpointPath()
	if err != nil {
		utils.Info.Printf("Warning: could not get checkpoint path: %v", err)
		checkpoint = &Checkpoint{Books: make(map[string]*CheckpointEntry)}
	} else {
		checkpoint, err = LoadCheckpoint(checkpointPath)
		if err != nil {
			utils.Info.Printf("Warning: could not load checkpoint: %v", err)
			checkpoint = &Checkpoint{Books: make(map[string]*CheckpointEntry)}
		}
	}

	// Phase 2: Parse + Match
	fmt.Printf("[2/4] Analyzing metadata for %d books...\n", len(books))

	type bookWithID struct {
		book     *models.BookSource
		identity *models.BookIdentity
	}

	var matched []bookWithID
	for i, book := range books {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(books), book.Name)

		// Check if this book is already in the checkpoint and still valid.
		// A "transferred" (real remote success) entry always counts as done.
		// A "local" entry only counts as done when this run is ALSO local-only —
		// otherwise a book that fell back to local (e.g. because the remote was
		// briefly unreachable) would never get a real remote transfer attempted
		// again on a later, non-local-only run.
		entry, exists := checkpoint.Books[checkpointKey(book)]
		checkpointDone := exists && (entry.TransferStatus == "transferred" || (entry.TransferStatus == "local" && cfg.LocalOnly))
		if checkpointDone {
			// Verify source files haven't changed
			currentSize, currentModTime := bookSourceStat(book)
			if currentSize == entry.SourceSize && currentModTime.Equal(entry.SourceModTime) {
				// Book is already transferred and source files are unchanged — skip re-processing
				result := models.TransferResult{
					SourceName: book.Name,
					Identity:   entry.Identity,
					Status:     entry.TransferStatus,
					FilesCount: entry.FilesCount,
					MethodUsed: entry.MethodUsed,
				}
				report.Results = append(report.Results, result)
				if entry.TransferStatus == "local" {
					report.Local++
				} else {
					report.Transferred++
				}
				continue
			}
		}

		// Determine parent context: the directory containing the book's files
		var parentName string
		if book.IsSingleFile {
			// For standalone files, the containing directory is the context
			parentName = filepath.Base(book.Path)
		} else {
			parentName = filepath.Base(filepath.Dir(book.Path))
		}
		// Skip source dir itself as parent context (eg "qbit" not an author)
		if parentName == filepath.Base(cfg.SourceDir) {
			parentName = ""
		}

		// Pass parent name for parsing context
		parsed := parser.ParseName(book.Name, parentName)

		// If parent is a series dir (Series (Author) pattern), inherit author/series
		if isSeriesPattern(parentName) {
			seriesParsed := parser.ParseName(parentName, "")
			if seriesParsed.Author != "" && parsed.Author == "" {
				parsed.Author = seriesParsed.Author
			}
			if seriesParsed.Series != "" && parsed.Series == "" {
				parsed.Series = seriesParsed.Series
			}
			parsed.Confidence = max(parsed.Confidence, 60)
		}

		// Structural evidence from the scanner (the top-level author folder the book
		// was discovered under) beats the parser's weak parent-name-as-author guess.
		// Only the parser's parent-name heuristic ever produces confidence <= 50 for
		// an author assignment; every filename-based match scores >= 65 (confirmed by
		// reading heuristicParse/regexParse/parseParentContext directly) — so a low
		// confidence author here reliably means it came from guessing the immediate
		// parent dir name, not the actual filename.
		if book.AuthorDir != "" && book.AuthorDir != parentName && parsed.Confidence <= 50 {
			if parser.IsAuthorish(book.AuthorDir) && !parser.IsTitleLike(book.AuthorDir) {
				parsed.Author = book.AuthorDir
				parsed.Confidence = 75
			}
		}

		identity := resolveIdentity(parsed, book, cfg)

		if identity != nil {
			matched = append(matched, bookWithID{book, identity})
		} else {
			result := models.TransferResult{
				SourceName: book.Name,
				Status:     "unmatched",
			}
			report.Results = append(report.Results, result)
			report.Unmatched++
		}
	}

	if len(matched) == 0 {
		if report.Transferred > 0 || report.Local > 0 {
			// Every book was already handled via the checkpoint fast-path —
			// nothing left to match/transfer, but this is success, not failure.
			report.PrintSummary()
		} else {
			fmt.Println("No books could be matched to identities.")
		}
		return report
	}

	// Phase 3: Confirm plan
	fmt.Printf("\n[3/4] Transfer plan (%d books):\n", len(matched))
	for _, m := range matched {
		fmt.Printf("  %s\n", m.identity.TargetPath())
		fmt.Printf("    %d audio files, %d cover files\n",
			len(m.book.AudioFiles), len(m.book.CoverFiles))
	}

	if !cfg.DryRun && !cfg.Force && cfg.Interactive {
		fmt.Print("\n  Proceed with transfer? (y/N): ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(response)) != "y" {
			fmt.Println("Transfer cancelled.")
			return report
		}
	}

	if cfg.DryRun {
		fmt.Println("\n[4/4] DRY RUN - no files transferred")
		for _, m := range matched {
			result := models.TransferResult{
				SourceName: m.book.Name,
				Identity:   m.identity,
				Status:     "skipped",
				FilesCount: len(m.book.AudioFiles) + len(m.book.CoverFiles),
			}
			report.Results = append(report.Results, result)
			report.Skipped++
		}
		return report
	}

	// Phase 4: Transfer
	fmt.Printf("\n[4/4] Transferring %d books...\n", len(matched))

	methodList := cfg.Methods
	if len(methodList) == 0 {
		if cfg.LocalOnly {
			methodList = []string{"local"}
		} else {
			methodList = transfer.TransferMethods
		}
	}

	for _, method := range methodList {
		client := transfer.NewClient(method, cfg.Host, cfg.TargetBase, cfg.SSHKeyPath, 22)

		fmt.Printf("\n  --- Trying method: %s ---\n", client.MethodName())
		report.MethodsTried = append(report.MethodsTried, method)

		if !client.Connect() {
			fmt.Printf("  [%s] Connection failed, trying next method...\n", client.MethodName())
			continue
		}

		// Build pending list (books not already transferred/local)
		var pending []bookWithID
		for _, m := range matched {
			alreadyDone := false
			for _, r := range report.Results {
				if r.SourceName == m.book.Name && (r.Status == "transferred" || r.Status == "local") {
					alreadyDone = true
					break
				}
			}
			if !alreadyDone {
				pending = append(pending, m)
			}
		}

		// Parallel transfer with bounded concurrency
		status := "transferred"
		if client.MethodName() == "local" {
			status = "local"
		}

		sem := make(chan struct{}, cfg.Parallel)
		resultsChan := make(chan models.TransferResult, len(pending))
		var wg sync.WaitGroup

		for _, m := range pending {
			wg.Add(1)
			go func(m bookWithID) {
				sem <- struct{}{}        // acquire semaphore slot
				defer func() { <-sem }() // release semaphore slot
				defer wg.Done()

				fmt.Printf("\n  [%s] %s\n", client.MethodName(), m.identity.TargetPath())

				var success bool
				if resumeSkip(client, m.book, m.identity) {
					utils.Info.Printf("  Skip (already on remote): %s", m.identity.TargetPath())
					success = true
				} else {
					success = client.TransferBook(
						m.book.AudioFiles,
						m.book.CoverFiles,
						m.identity.TargetPath(),
					)
				}

				result := models.TransferResult{
					SourceName: m.book.Name,
					Identity:   m.identity,
					Status:     status,
					FilesCount: len(m.book.AudioFiles) + len(m.book.CoverFiles),
					MethodUsed: method,
				}

				if !success {
					result.Status = "failed"
					result.Error = "Transfer failed"
				}

				resultsChan <- result
			}(m)
		}

		// Wait for all workers and close results channel
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Read results and update report (single-threaded to avoid mutex on report.Results)
		anySuccess := false
		for result := range resultsChan {
			// Remove old results for same book (replaces, not duplicates)
			for i, r := range report.Results {
				if r.SourceName == result.SourceName {
					report.Results = append(report.Results[:i], report.Results[i+1:]...)
					break
				}
			}
			report.Results = append(report.Results, result)

			if result.Status == "transferred" || result.Status == "local" {
				anySuccess = true
				if result.Status == "local" {
					report.Local++
				} else {
					report.Transferred++
				}
			}

			// Update checkpoint on success
			if result.Status == "transferred" || result.Status == "local" {
				for _, m := range pending {
					if m.book.Name == result.SourceName {
						currentSize, currentModTime := bookSourceStat(m.book)
						checkpoint.Books[checkpointKey(m.book)] = &CheckpointEntry{
							Identity:       result.Identity,
							TransferStatus: result.Status,
							MethodUsed:     result.MethodUsed,
							TransferredAt:  time.Now(),
							FilesCount:     result.FilesCount,
							SourceSize:     currentSize,
							SourceModTime:  currentModTime,
						}
						if checkpointPath != "" {
							if err := SaveCheckpoint(checkpointPath, checkpoint); err != nil {
								utils.Info.Printf("Warning: could not save checkpoint: %v", err)
							}
						}
						break
					}
				}
			}
		}

		if anySuccess {
			fmt.Printf("  [%s] Transferred some books successfully\n", client.MethodName())
		}
		client.Disconnect()

		// Check if all done
		done := true
		for _, m := range matched {
			transferred := false
			for _, r := range report.Results {
				if r.SourceName == m.book.Name && (r.Status == "transferred" || r.Status == "local") {
					transferred = true
					break
				}
			}
			if !transferred {
				done = false
				break
			}
		}
		if done {
			break
		}
	}

	// Phase 5: Verify (if requested)
	if cfg.Verify && !cfg.DryRun {
		fmt.Printf("\n[5/5] Verifying transfers...\n")
		verifyTransfers(report, cfg)
	}

	// Count failures — only if book has no success result (verify already incremented failed, reset here)
	report.Failed = 0
	for _, m := range matched {
		hasSuccess := false
		for _, r := range report.Results {
			if r.SourceName == m.book.Name && (r.Status == "transferred" || r.Status == "local") {
				hasSuccess = true
				break
			}
		}
		if !hasSuccess {
			for _, r := range report.Results {
				if r.SourceName == m.book.Name && r.Status == "failed" {
					report.Failed++
					break
				}
			}
		}
	}

	report.PrintSummary()

	// Hint for local-only
	if report.Local > 0 && report.Transferred == 0 {
		fmt.Println("\n  All books organized locally.")
		fmt.Printf("  Manual transfer:\n    rsync -avzP %s/ root@%s:%s/\n",
			cfg.TargetBase, cfg.Host, cfg.TargetBase)
	}

	return report
}

// verifyTransfers verifies transferred files exist on target.
func verifyTransfers(report *models.TransferReport, cfg Config) {
	// Reuse one connected client per method across every book being verified,
	// instead of creating (and never even Connect()-ing) a throwaway client per
	// book. Without Connect() being called, controlSocket is never populated so
	// SSH multiplexing never kicks in for verification — this was the actual
	// cause of the false-negative "MISSING" wall of results tonight, since every
	// verify call opened its own independent, unauthenticated-connection-reused
	// ssh subprocess in rapid succession.
	clients := make(map[string]transfer.TransferClient)
	defer func() {
		for _, c := range clients {
			c.Disconnect()
		}
	}()

	for _, r := range report.Results {
		if r.Status != "transferred" && r.Status != "local" {
			continue
		}
		if r.Identity == nil {
			continue
		}

		method := r.MethodUsed
		if method == "" {
			continue
		}

		client, ok := clients[method]
		if !ok {
			client = transfer.NewClient(method, cfg.Host, cfg.TargetBase, cfg.SSHKeyPath, 22)
			if !client.Connect() {
				utils.Warn.Printf("  Could not connect to verify via %s; skipping remaining %s verifications", method, method)
				continue
			}
			clients[method] = client
		}

		v := verifyWithRetry(client, r.Identity.TargetPath(), 3)

		if exists, ok := v["exists"].(bool); ok && exists {
			files, _ := v["files"].([]map[string]interface{})
			totalSize, _ := v["total_size"].(int64)
			fmt.Printf("  OK: %s (%d files, %s)\n", v["path"], len(files), transfer.FormatSize(totalSize))
		} else {
			errMsg := "unknown"
			if e, ok := v["error"].(string); ok {
				errMsg = e
			}
			fmt.Printf("  MISSING: %s (%s)\n", r.Identity.TargetPath(), errMsg)

			originalStatus := r.Status
			r.Status = "failed"
			r.Error = fmt.Sprintf("Verification failed: %s", errMsg)

			if originalStatus == "transferred" && report.Transferred > 0 {
				report.Transferred--
			} else if originalStatus == "local" && report.Local > 0 {
				report.Local--
			}
			report.Failed++
		}
	}
}

// verifyWithRetry calls VerifyTransfer, retrying on a transient connection
// failure (connection_error=true) but not on a genuine "path not found" result
// (connection_error=false) — those are real and retrying them wastes time.
func verifyWithRetry(client transfer.TransferClient, targetPath string, attempts int) map[string]interface{} {
	var v map[string]interface{}
	for i := 0; i < attempts; i++ {
		v = client.VerifyTransfer(targetPath)
		connErr, _ := v["connection_error"].(bool)
		if !connErr {
			return v
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
		}
	}
	return v
}

// resumeSkip reports whether a book's files already exist on the remote target
// with matching total size, so an interrupted run can resume without
// re-uploading books that already completed.
func resumeSkip(client transfer.TransferClient, book *models.BookSource, identity *models.BookIdentity) bool {
	var localSize int64
	for _, f := range append(append([]string{}, book.AudioFiles...), book.CoverFiles...) {
		if fi, err := os.Stat(f); err == nil {
			localSize += fi.Size()
		}
	}
	if localSize == 0 {
		return false
	}

	v := client.VerifyTransfer(identity.TargetPath())
	exists, _ := v["exists"].(bool)
	if !exists {
		return false
	}
	remoteSize, _ := v["total_size"].(int64)
	return remoteSize == localSize
}

// resolveIdentity resolves a book identity from parsed info + optional API enrichment.
func resolveIdentity(parsed *models.ParsedInfo, book *models.BookSource, cfg Config) *models.BookIdentity {
	identity := &models.BookIdentity{
		Title:          parsed.Title,
		Author:         parsed.Author,
		Series:         parsed.Series,
		SeriesPosition: parsed.SeriesPosition,
		Confidence:     parsed.Confidence,
		MetadataSources: []string{"filename"},
	}

	// Only fall back to "series as title" when the parser produced no title.
	// Previously this triggered whenever the author was unknown, which clobbered
	// real titles for files like "Series_Title -- Subtitle [ASIN]".
	if parsed.Author == "" && parsed.Series != "" && identity.Title == "" {
		identity.Title = parsed.Series
		identity.Confidence = max(identity.Confidence, 50)
	}

	// Try Open Library API enrichment
	if cfg.lookupMetadata() && (identity.Title != "" || identity.Author != "") {
		ol := metadata.Lookup(identity.Title, identity.Author)
		if ol != nil {
			identity.MetadataSources = append(identity.MetadataSources, "openlibrary")
			identity.Confidence += 15

			if identity.Author == "" && ol.Author != "" {
				identity.Author = ol.Author
			}
			if identity.Title == "" && ol.Title != "" {
				identity.Title = ol.Title
			}
			if ol.Year > 0 {
				identity.Confidence += 5
			}
		}
	}

	// Fallbacks
	if identity.Author == "" && book.Name != "" {
		// Try parsing parent dir for author context (exclude source dir itself)
		authorFromParent := extractAuthorFromPath(book.Path, cfg.SourceDir)
		if authorFromParent != "" {
			identity.Author = authorFromParent
			identity.Confidence = max(identity.Confidence, 25)
		}
	}

	if identity.Author == "" {
		identity.Author = "Unknown"
		identity.Confidence = max(identity.Confidence, 5)
	}
	if identity.Title == "" {
		identity.Title = book.Name
		identity.Confidence = max(identity.Confidence, 5)
	}

	identity.Author = strings.TrimSpace(identity.Author)
	identity.Title = strings.TrimSpace(identity.Title)
	if identity.Series != "" {
		identity.Series = strings.TrimSpace(identity.Series)
	}

	if identity.Confidence > 100 {
		identity.Confidence = 100
	}

	utils.Info.Printf("  Resolved: %s / %s / %s (conf: %d%%)",
		identity.Author, coalesce(identity.Series, "-"), identity.Title, identity.Confidence)

	return identity
}

// extractAuthorFromPath tries to determine an author from the directory path structure.
// Walks up the directory tree from deepest level, finds first author-like directory.
// Stops at the source directory boundary to avoid picking up source dir name as author.
func extractAuthorFromPath(path string, sourceDir string) string {
	sourceDir = filepath.Clean(sourceDir)
	dir := filepath.Dir(path)
	parts := strings.Split(dir, string(filepath.Separator))
	sourceParts := strings.Split(sourceDir, string(filepath.Separator))

	for i := len(parts) - 1; i >= 0; i-- {
		segment := strings.TrimSpace(parts[i])
		if segment == "" || segment == "." || segment == "/" || segment == "audiobooks" || segment == "audiobook" {
			continue
		}
		// Stop at source directory boundary
		if i < len(sourceParts) && parts[i] == sourceParts[i] {
			// Check if we've reached the source dir prefix
			match := true
			for j := i; j < min(len(parts), len(sourceParts)); j++ {
				if parts[j] != sourceParts[j] {
					match = false
					break
				}
			}
			if match {
				break
			}
		}
		// Skip segments that look like titles (e.g. series names)
		if parser.IsTitleLike(segment) {
			continue
		}
		// Check if this segment is directly an author name
		if parser.IsAuthorLike(segment) {
			return segment
		}
		// Check " - " pattern: "Author - Title" where author is before dash
		if idx := strings.Index(segment, " - "); idx >= 0 {
			potentialAuthor := strings.TrimSpace(segment[:idx])
			if parser.IsAuthorish(potentialAuthor) && !parser.IsTitleLike(potentialAuthor) {
				return potentialAuthor
			}
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c Config) lookupMetadata() bool {
	return !c.DryRun
}

func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// isSeriesPattern checks if a directory name follows "Series (Author)" pattern.
func isSeriesPattern(name string) bool {
	lastOpen := strings.LastIndex(name, "(")
	if lastOpen < 0 {
		return false
	}
	lastClose := strings.LastIndex(name, ")")
	if lastClose <= lastOpen {
		return false
	}
	before := strings.TrimSpace(name[:lastOpen])
	return !strings.Contains(before, " - ")
}
