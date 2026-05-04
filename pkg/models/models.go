// Package models provides shared data types for the audioTransfer tool.
package models

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BookSource represents one audiobook source (directory or single file).
type BookSource struct {
	Name         string   // Directory/filename
	Path         string   // Full path
	AudioFiles   []string // List of audio file paths
	CoverFiles   []string // List of cover image paths
	IsSingleFile bool     // True if just one .m4b/.mp3 file
	IsFromZip    bool     // True if extracted from zip
}

// ParsedInfo holds parsed metadata from filename/directory.
type ParsedInfo struct {
	RawName        string
	Title          string
	Author         string
	Series         string
	SeriesPosition float64
	ASIN           string
	Narrator       string
	Year           int
	Confidence     int
	Extra          map[string]string
}

// BookMetadata holds enriched book metadata from APIs.
type BookMetadata struct {
	Title          string
	Author         string
	Series         string
	SeriesPosition float64
	ASIN           string
	Narrator       string
	Year           int
	Description    string
	CoverURL       string
	OLWorkKey      string
	OLAuthorKey    string
	Confidence     int
	Source         string
	Raw            map[string]interface{}
}

// BookIdentity is the final resolved book identity for transfer.
type BookIdentity struct {
	Title           string
	Author          string
	Series          string
	SeriesPosition  float64
	Confidence      int
	MetadataSources []string
}

// TargetPath builds the target path: author/series/book or author/book.
func (b *BookIdentity) TargetPath() string {
	authorDir := SanitizeName(b.Author)
	bookDir := SanitizeName(b.Title)
	if b.Series != "" {
		seriesDir := SanitizeName(b.Series)
		return filepath.Join(authorDir, seriesDir, bookDir)
	}
	return filepath.Join(authorDir, bookDir)
}

// TransferResult holds the result of transferring one book.
type TransferResult struct {
	SourceName string
	Identity   *BookIdentity
	Status     string // pending, transferred, skipped, failed, unmatched, local
	Error      string
	FilesCount int
	TotalBytes int64
	MethodUsed string
}

// TransferReport holds summary of a transfer session.
type TransferReport struct {
	Total        int
	Transferred  int
	Skipped      int
	Failed       int
	Unmatched    int
	Local        int
	Results      []TransferResult
	MethodsTried []string
}

// PrintSummary prints a summary of the transfer session.
func (r *TransferReport) PrintSummary() {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  TRANSFER SUMMARY")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  Total books scanned : %d\n", r.Total)
	fmt.Printf("  Transferred (remote): %d\n", r.Transferred)
	fmt.Printf("  Copied (local)      : %d\n", r.Local)
	fmt.Printf("  Skipped             : %d\n", r.Skipped)
	fmt.Printf("  Failed              : %d\n", r.Failed)
	fmt.Printf("  Unmatched           : %d\n", r.Unmatched)
	if len(r.MethodsTried) > 0 {
		fmt.Printf("  Transfer methods    : %s\n", strings.Join(r.MethodsTried, " -> "))
	}
	fmt.Println(strings.Repeat("=", 60))

	if r.Failed > 0 {
		fmt.Println("\n  Failed transfers:")
		for _, res := range r.Results {
			if res.Status == "failed" {
				fmt.Printf("    - %s: %s\n", res.SourceName, res.Error)
			}
		}
	}
}

// SanitizeName sanitizes a name for use as a directory path component.
func SanitizeName(name string) string {
	unsafeChars := `<>:"/\|?*`
	for _, ch := range unsafeChars {
		name = strings.ReplaceAll(name, string(ch), " ")
	}
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.Trim(name, ". ")
	for strings.Contains(name, "  ") {
		name = strings.ReplaceAll(name, "  ", " ")
	}
	if name == "" {
		return "Unknown"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return strings.TrimRight(name, ". ")
}

// NormalizeAuthor converts "Last, First" to "First Last".
func NormalizeAuthor(name string) string {
	name = strings.TrimSpace(name)
	if strings.Contains(name, ",") {
		parts := strings.Split(name, ",")
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
		}
	}
	return name
}
