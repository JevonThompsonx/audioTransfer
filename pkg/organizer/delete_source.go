package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

// isPathWithin reports whether p is a strict child of root (both absolute,
// cleaned). p == root or p outside root => false.
func isPathWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// samePath reports whether two paths resolve to the same cleaned absolute path.
func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// isDeletionSafe enforces the SourceDir strict-child AND DestDir exclusion
// guards for a single deletion target.
func isDeletionSafe(target string, cfg Config) bool {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	src, _ := filepath.Abs(cfg.SourceDir)
	if !isPathWithin(abs, src) {
		return false // outside SourceDir
	}
	if cfg.DestDir != "" {
		dst, _ := filepath.Abs(cfg.DestDir)
		if isPathWithin(abs, dst) || samePath(abs, dst) {
			return false // would delete the organized copy
		}
	}
	return true
}

// deleteFootprint returns the exact absolute paths to delete for one book,
// enforcing containment guards. Returns nil when there is nothing safe to delete.
func deleteFootprint(book *models.BookSource, cfg Config) []string {
	if book == nil {
		return nil
	}

	// Zip books: the extracted temp dir is removed by scanner cleanup and the
	// source .zip is handled separately by deleteEligibleZips.
	if book.IsFromZip {
		return nil
	}

	// Single-file book: own audio + cover files, plus a same-directory cover
	// whose stem matches the audio stem (unambiguous ownership). Never a
	// generic cover.jpg that could be shared with sibling books.
	if book.IsSingleFile {
		var paths []string
		seen := make(map[string]bool)
		add := func(p string) {
			if p != "" && !seen[p] && isDeletionSafe(p, cfg) {
				seen[p] = true
				paths = append(paths, p)
			}
		}
		for _, f := range book.AudioFiles {
			add(f)
			for _, c := range coLocatedCovers(f) {
				add(c)
			}
		}
		for _, f := range book.CoverFiles {
			add(f)
		}
		return paths
	}

	// Directory book: delete the leaf book directory book.Path, but only if
	// every audio/cover file is inside it. Defensive fallback: if the scanner
	// ever attributes a file outside book.Path, delete just that book's own
	// files instead of the whole directory.
	if book.Path == "" {
		return nil
	}
	allInside := true
	for _, f := range append(append([]string{}, book.AudioFiles...), book.CoverFiles...) {
		if !isPathWithin(filepath.Clean(f), filepath.Clean(book.Path)) {
			allInside = false
			break
		}
	}
	if allInside {
		if isDeletionSafe(book.Path, cfg) {
			return []string{book.Path}
		}
		return nil
	}
	var paths []string
	seen := make(map[string]bool)
	for _, f := range append(append([]string{}, book.AudioFiles...), book.CoverFiles...) {
		if f != "" && !seen[f] && isDeletionSafe(f, cfg) {
			seen[f] = true
			paths = append(paths, f)
		}
	}
	return paths
}

// coLocatedCovers returns same-dir cover images sharing the audio file's stem.
func coLocatedCovers(audioFile string) []string {
	dir := filepath.Dir(audioFile)
	stem := strings.TrimSuffix(filepath.Base(audioFile), filepath.Ext(audioFile))
	if stem == "" {
		return nil
	}
	var covers []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !utils.IsCover(filepath.Join(dir, name)) {
			continue
		}
		if strings.TrimSuffix(name, filepath.Ext(name)) == stem {
			covers = append(covers, filepath.Join(dir, name))
		}
	}
	return covers
}

// deletePaths removes each path (os.RemoveAll works for files and dirs).
func deletePaths(paths []string) error {
	var firstErr error
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// hasSuccessfulResult reports whether report has a transferred/local result for name.
func hasSuccessfulResult(name string, report *models.TransferReport) bool {
	if report == nil {
		return false
	}
	for _, r := range report.Results {
		if r.SourceName == name && (r.Status == "transferred" || r.Status == "local") {
			return true
		}
	}
	return false
}

// markSourceDeleted sets SourceDeleted and bumps report.Deleted.
func markSourceDeleted(name string, report *models.TransferReport) {
	if report == nil {
		return
	}
	for i := range report.Results {
		if report.Results[i].SourceName == name {
			report.Results[i].SourceDeleted = true
		}
	}
	report.Deleted++
}

// deleteSourceFiles deletes local sources for every successfully transferred
// book. Call only when cfg.DeleteSource && !cfg.DryRun, AFTER verifyTransfers.
func deleteSourceFiles(cfg Config, matched []bookWithID, report *models.TransferReport) {
	if report == nil {
		return
	}

	// Opt-in and never-in-dry-run guards (defense in depth on top of the
	// call-site check in RunTransfer).
	if !cfg.DeleteSource || cfg.DryRun {
		return
	}

	// Local-only guard: never allow deletion when source == dest (would wipe
	// the organized copy). Fail the run instead of half-deleting.
	if cfg.LocalOnly && samePath(cfg.SourceDir, cfg.DestDir) {
		fmt.Printf("ERROR: --delete-source with --local requires SourceDir != DestDir; source deletion disabled for this run\n")
		report.Failed++
		return
	}

	for _, m := range matched {
		if !hasSuccessfulResult(m.book.Name, report) {
			continue
		}
		paths := deleteFootprint(m.book, cfg)
		if len(paths) == 0 {
			continue
		}
		if err := deletePaths(paths); err != nil {
			utils.Warn.Printf("  Failed to delete source for %s: %v", m.book.Name, err)
			continue
		}
		markSourceDeleted(m.book.Name, report)
	}
}

// deleteEligibleZips removes a source .zip only when every book extracted from
// it has a successful result and the zip passes isDeletionSafe. Inert today
// (see TRANSFER_FIXES_PLAN §0) but safe.
func deleteEligibleZips(cfg Config, matched []bookWithID, report *models.TransferReport) {
	if report == nil {
		return
	}
	// Opt-in and never-in-dry-run guards.
	if !cfg.DeleteSource || cfg.DryRun {
		return
	}
	byZip := make(map[string][]bookWithID)
	for _, m := range matched {
		if m.book.ZipPath == "" {
			continue
		}
		byZip[m.book.ZipPath] = append(byZip[m.book.ZipPath], m)
	}
	for zipPath, books := range byZip {
		allDone := true
		for _, m := range books {
			if !hasSuccessfulResult(m.book.Name, report) {
				allDone = false
				break
			}
		}
		if !allDone {
			continue
		}
		if !isDeletionSafe(zipPath, cfg) {
			continue
		}
		if err := deletePaths([]string{zipPath}); err != nil {
			utils.Warn.Printf("  Failed to delete source zip %s: %v", zipPath, err)
		}
	}
}
