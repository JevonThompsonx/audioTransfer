// Package organizer orchestrates the full audiobook scan→parse→match→transfer pipeline.
package organizer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// Phase 2: Parse + Match
	fmt.Printf("[2/4] Analyzing metadata for %d books...\n", len(books))

	type bookWithID struct {
		book     *models.BookSource
		identity *models.BookIdentity
	}

	var matched []bookWithID
	for i, book := range books {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(books), book.Name)

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
		fmt.Println("No books could be matched to identities.")
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

		anySuccess := false
		for _, m := range matched {
			// Skip already transferred
			alreadyDone := false
			for _, r := range report.Results {
				if r.SourceName == m.book.Name && (r.Status == "transferred" || r.Status == "local") {
					alreadyDone = true
					break
				}
			}
			if alreadyDone {
				continue
			}

			status := "transferred"
			if client.MethodName() == "local" {
				status = "local"
			}

			fmt.Printf("\n  [%s] %s\n", client.MethodName(), m.identity.TargetPath())

			success := client.TransferBook(
				m.book.AudioFiles,
				m.book.CoverFiles,
				m.identity.TargetPath(),
			)

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

			// Remove old results for same book (replaces, not duplicates)
			for i, r := range report.Results {
				if r.SourceName == m.book.Name {
					report.Results = append(report.Results[:i], report.Results[i+1:]...)
					break
				}
			}
			report.Results = append(report.Results, result)

			if success {
				anySuccess = true
				if status == "local" {
					report.Local++
				} else {
					report.Transferred++
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

		client := transfer.NewClient(method, cfg.Host, cfg.TargetBase, cfg.SSHKeyPath, 22)
		v := client.VerifyTransfer(r.Identity.TargetPath())

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

	if parsed.Author == "" && parsed.Series != "" {
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
