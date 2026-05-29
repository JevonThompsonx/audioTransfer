// Package parser provides filename parsing for audiobook naming patterns.
// Combines regex patterns and heuristic matching for maximum coverage.
package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

// pattern represents a regex pattern with field mappings.
type pattern struct {
	re      *regexp.Regexp
	mapping map[string]int // field_name -> group_index (1-based)
}

// Regex patterns covering common audiobook naming conventions.
var regexPatterns = []pattern{
	// Author - Series, Book N - Title
	{regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+?),\s*Book\s*([\d.]+)\s*[-–—]\s*(.+)$`),
		map[string]int{"author": 1, "series": 2, "series_position": 3, "title": 4, "_conf": 90}},

	// Author - Series, Book N
	{regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+?),\s*Book\s*([\d.]+)$`),
		map[string]int{"author": 1, "series": 2, "series_position": 3, "_conf": 80}},

	// Author - Title [ASIN]
	{regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+?)\s*\[([A-Z0-9]{10})\]$`),
		map[string]int{"author": 1, "title": 2, "asin": 3, "_conf": 85}},

	// [NN] Title (numbered series entry)
	{regexp.MustCompile(`^\[(\d+)\]\s*(.+)$`),
		map[string]int{"series_position": 1, "title": 2, "_conf": 60}},

	// NN Title (no brackets)
	{regexp.MustCompile(`^(\d+)\s+(.+)$`),
		map[string]int{"series_position": 1, "title": 2, "_conf": 60}},

	// Title [ASIN]
	{regexp.MustCompile(`^(.+?)\s*\[([A-Z0-9]{10})\]$`),
		map[string]int{"title": 1, "asin": 2, "_conf": 65}},

	// Word NN - Title (series with position, e.g. "Pern 01 - Dragonflight")
	{regexp.MustCompile(`^([A-Za-z]+)\s+(\d+(?:\.\d+)?)\s+[-–—]\s+(.+)$`),
		map[string]int{"series": 1, "series_position": 2, "title": 3, "_conf": 80}},

	// Author - Title (standard, lower confidence; requires space around dash to avoid kebab-case)
	{regexp.MustCompile(`^(.+?)\s+[-–—]\s+(.+)$`),
		map[string]int{"author": 1, "title": 2, "_conf": 70}},
}

// Audio extensions to strip before parsing.
var audioExts = []string{".m4b", ".m4a", ".mp3", ".aax", ".ogg", ".wma", ".flac", ".wav", ".aac", ".m4p", ".zip"}

// Words that indicate the string is NOT an author name.
var nonAuthorWords = []string{
	"gothic horror", "chapterized", "series", "unabridged", "audiobook",
	"radio drama", "book", "entangled with fae", "beauty and the beast",
	"retelling", "abridged", "dramatized", "omnibus", "collection",
	"edition", "version", "narrated", "vol.", "volume", "trilogy",
	"saga", "chronicles", "cycle", "anthology",
}

// Title indicator words for reverse pattern detection.
var titleWords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"with": true, "from": true, "by": true, "or": true, "it": true,
	"is": true, "no": true, "not": true,
}

// ParseName parses an audiobook filename/directory name into metadata.
// parentName provides context from the parent directory.
func ParseName(name string, parentName string) *models.ParsedInfo {
	clean := strings.TrimSpace(name)

	// Strip audio/image extensions
	lower := strings.ToLower(clean)
	for _, ext := range audioExts {
		if strings.HasSuffix(lower, ext) {
			clean = clean[:len(clean)-len(ext)]
			break
		}
	}
	// Also strip image extensions
	for ext := range utils.CoverExts() {
		if strings.HasSuffix(strings.ToLower(clean), ext) {
			clean = clean[:len(clean)-len(ext)]
			break
		}
	}

	info := &models.ParsedInfo{
		RawName: name,
		Extra:   make(map[string]string),
	}

	// First pass: try heuristic parsing (handles Series (Author), reverse patterns)
	heuristicParse(clean, info)

	// Second pass: if regex gives us more info, use it
	regexParse(clean, info)

	// Post-process: inherit author from parent if parent looks like author name
	if info.Author == "" && parentName != "" && !strings.Contains(parentName, "/") {
		parentClean := parentName
		authorToUse := parentName
		// Handle comma-separated multi-author: "Caroline Peckham, Susanne Valenti"
		// Use first author for inheritance check and as the author value
		if strings.Contains(parentClean, ",") {
			parentClean = strings.TrimSpace(strings.Split(parentClean, ",")[0])
			authorToUse = parentClean
		}
		if isAuthorish(parentClean) && !isTitleLike(parentClean) {
			words := strings.Fields(authorToUse)
			if len(words) <= 4 {
				info.Author = authorToUse
				if info.Confidence < 45 {
					info.Confidence = 45
				}
			}
		}
	}

	// Post-process: extract series position from Volume/Vol NN anywhere in title, remove from title
	if info.SeriesPosition == 0 && info.Title != "" {
		volRe := regexp.MustCompile(`(?i)\s*(?:Volume|Vol\.?)\s*(\d+(?:\.\d+)?)`)
		if loc := volRe.FindStringSubmatchIndex(info.Title); loc != nil {
			if v, err := strconv.ParseFloat(info.Title[loc[2]:loc[3]], 64); err == nil {
				info.SeriesPosition = v
				// Remove volume text from title
				info.Title = strings.TrimSpace(info.Title[:loc[0]] + info.Title[loc[1]:])
				if info.Confidence < 55 {
					info.Confidence = 55
				}
			}
		}
		// Also handle [Volume NN] bracketed
		bracketRe := regexp.MustCompile(`(?i)\s*\[(?:Volume|Vol\.?)\s*(\d+(?:\.\d+)?)\]`)
		if m := bracketRe.FindStringSubmatch(info.Title); m != nil && info.SeriesPosition == 0 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				info.SeriesPosition = v
				info.Title = bracketRe.ReplaceAllString(info.Title, "")
				if info.Confidence < 55 {
					info.Confidence = 55
				}
			}
		}
	}

	// Post-process: clean up title after volume removal
	if info.Title != "" {
		// Remove trailing comma, dot, semicolon from volume removal artifacts
		info.Title = strings.TrimRight(info.Title, " ,;.")
		// Remove empty brackets
		info.Title = regexp.MustCompile(`\[\s*\]`).ReplaceAllString(info.Title, "")
		info.Title = strings.TrimSpace(info.Title)
		// Strip [PZG] or similar release group tags from start
		info.Title = regexp.MustCompile(`^\[[A-Z0-9]+\]\s*`).ReplaceAllString(info.Title, "")
		info.Title = strings.TrimSpace(info.Title)
		// Strip (Audiobook) (Unabridged) etc from end
		info.Title = regexp.MustCompile(`(?i)\s*\((?:Audiobook|Unabridged|Unabr)\)`).ReplaceAllString(info.Title, "")
		info.Title = regexp.MustCompile(`(?i)\s*\{[^}]*\}`).ReplaceAllString(info.Title, "") // {narrator} tags
		info.Title = strings.TrimSpace(info.Title)
	}

	// Post-process: extract title from "Series NN - Title" pattern
	if info.Series == "" && info.Title != "" {
		seriesRe := regexp.MustCompile(`^(\w+)\s+(\d+(?:\.\d+)?)\s+[-–—]\s+(.+)$`)
		if m := seriesRe.FindStringSubmatch(info.Title); m != nil {
			info.Series = m[1]
			if v, err := strconv.ParseFloat(m[2], 64); err == nil {
				info.SeriesPosition = v
			}
			info.Title = m[3]
			if info.Confidence < 65 {
				info.Confidence = 65
			}
		}
	}

	// Post-processing
	if info.Author != "" {
		info.Author = models.NormalizeAuthor(info.Author)
	}

	// Extract parent context
	parseParentContext(info, parentName)

	// Extract ASIN from anywhere
	extractASIN(clean, info)

	return info
}

// heuristicParse handles non-standard patterns that regex misses.
func heuristicParse(clean string, info *models.ParsedInfo) {
	// Pattern: "Series Name (Author)" — author in last parenthetical
	if lastOpen := strings.LastIndex(clean, "("); lastOpen >= 0 {
		lastClose := strings.LastIndex(clean, ")")
		if lastClose > lastOpen {
			before := strings.TrimSpace(clean[:lastOpen])
			parenContent := strings.TrimSpace(clean[lastOpen+1 : lastClose])

			// Strip all parentheticals from before
			before = stripAllParens(before)

			if !strings.Contains(before, " - ") && isAuthorish(parenContent) {
				info.Author = parenContent
				info.Series = before // Treat the "Series (Author)" format
				if info.Series != "" && !strings.ContainsAny(before, " -–—") {
					info.Confidence = maxInt(info.Confidence, 75)
				}
				return
			} else {
				// Author - Title (publisher info)
				clean = before
			}
		} else {
			clean = stripAllParens(clean)
		}
	} else {
		clean = stripAllParens(clean)
	}

	// Pattern: "Author - Title" or "Title - Author" via heuristics
	if strings.Contains(clean, " - ") {
		idx := strings.Index(clean, " - ")
		left := strings.TrimSpace(clean[:idx])
		right := strings.TrimSpace(clean[idx+3:])

		// Check for reverse pattern (Title - Author)
		if isAuthorName(right) && !isAuthorName(left) {
			info.Author = right
			info.Title = left
			info.Confidence = maxInt(info.Confidence, 70)
			return
		}

		// Standard: Author - Title
		info.Author = left
		info.Title = stripAllParens(right)
		info.Confidence = maxInt(info.Confidence, 65)
		return
	}

	// Fallback: use entire string as title
	if info.Title == "" {
		info.Title = clean
		if info.Confidence < 30 {
			info.Confidence = 30
		}
	}
}

// regexParse tries structured regex patterns against the clean name.
func regexParse(clean string, info *models.ParsedInfo) {
	for _, p := range regexPatterns {
		match := p.re.FindStringSubmatch(clean)
		if match == nil {
			continue
		}

		// Only override heuristic results if regex confidence is higher
		regexConf := 0
		if c, ok := p.mapping["_conf"]; ok {
			regexConf = c
		}
		if regexConf > info.Confidence {
			// Clear previous results for a clean match
			info.Title = ""
			info.Author = ""
			info.Series = ""
			info.SeriesPosition = 0
		}

		for fieldName, groupIdx := range p.mapping {
			if fieldName == "_conf" {
				info.Confidence = maxInt(info.Confidence, groupIdx)
			} else if groupIdx < len(match) {
				value := strings.TrimSpace(match[groupIdx])
				switch fieldName {
				case "title":
					if info.Title == "" {
						info.Title = value
					}
				case "author":
					if info.Author == "" {
						info.Author = value
					}
				case "series":
					if info.Series == "" {
						info.Series = value
					}
				case "series_position":
					if info.SeriesPosition == 0 {
						if v, err := strconv.ParseFloat(value, 64); err == nil {
							info.SeriesPosition = v
						}
					}
				case "asin":
					if info.ASIN == "" {
						info.ASIN = value
					}
				case "year":
					if info.Year == 0 {
						if v, err := strconv.Atoi(value); err == nil {
							info.Year = v
						}
					}
				}
			}
		}
		break
	}
}

// parseParentContext extracts series/author from the parent directory name.
func parseParentContext(info *models.ParsedInfo, parentName string) {
	if parentName == "" {
		return
	}

	// Try "Author - ..." pattern from parent for author
	if info.Author == "" {
		parentClean := stripAllParens(parentName)
		re := regexp.MustCompile(`^([^-–—]+?)\s*[-–—]\s+`)
		if m := re.FindStringSubmatch(parentClean); m != nil {
			info.Author = models.NormalizeAuthor(strings.TrimSpace(m[1]))
			if info.Confidence < 40 {
				info.Confidence = 40
			}
		}
	}

	// Try "Series (Author)" pattern from parent for series/author
	if info.Series == "" || info.Author == "" {
		if lastOpen := strings.LastIndex(parentName, "("); lastOpen >= 0 {
			lastClose := strings.LastIndex(parentName, ")")
			if lastClose > lastOpen {
				before := strings.TrimSpace(parentName[:lastOpen])
				parenContent := strings.TrimSpace(parentName[lastOpen+1 : lastClose])
				if isAuthorish(parenContent) && !strings.Contains(before, " - ") {
					if info.Author == "" {
						info.Author = models.NormalizeAuthor(parenContent)
						if info.Confidence < 50 {
							info.Confidence = 50
						}
					}
					if info.Series == "" && before != "" {
						info.Series = before
						if info.Confidence < 50 {
							info.Confidence = 50
						}
					}
					return
				}
			}
		}
	}

	// Look for series keywords in parent name
	keywordRe := regexp.MustCompile(`(?i)\s+(Series|Saga|Trilogy|Cycle|Chronicles|books\s+\d+)`)
	keywordMatch := keywordRe.FindStringIndex(parentName)

	if keywordMatch != nil && info.Series == "" {
		before := strings.TrimSpace(parentName[:keywordMatch[0]])
		words := strings.Fields(before)

		if len(words) > 0 {
			// Don't extract author from parent if before looks like title (starts with The/A/An)
			if info.Author == "" && len(words) >= 3 && len(words) <= 5 {
				lower := strings.ToLower(before)
				if !strings.HasPrefix(lower, "the ") && !strings.HasPrefix(lower, "a ") && !strings.HasPrefix(lower, "an ") {
					potentialAuthor := words[0] + " " + words[1]
					if isAuthorName(potentialAuthor) {
						info.Author = potentialAuthor
						if info.Confidence < 35 {
							info.Confidence = 35
						}
						info.Series = strings.Join(words[2:], " ")
						if info.Confidence < 40 {
							info.Confidence = 40
						}
						return
					}
				}
			}

			// Fallback: use entire before as series
			info.Series = before
			if info.Confidence < 40 {
				info.Confidence = 40
			}
		}
	}
}

// extractASIN finds an ASIN anywhere in the string.
func extractASIN(clean string, info *models.ParsedInfo) {
	if info.ASIN != "" {
		return
	}
	asinRe := regexp.MustCompile(`\[([A-Z0-9]{10})\]$`)
	if m := asinRe.FindStringSubmatch(clean); m != nil {
		info.ASIN = m[1]
		if info.Confidence < 60 {
			info.Confidence = 60
		}
	}
}

// isAuthorish checks if a string looks like a person's name (heuristic).
func isAuthorish(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	words := strings.Fields(s)
	if len(words) < 1 || len(words) > 5 {
		return false
	}
	for _, w := range words {
		if len(w) > 15 {
			return false
		}
	}
	lower := strings.ToLower(s)
	for _, na := range nonAuthorWords {
		if strings.Contains(lower, na) {
			return false
		}
	}
	return true
}

// isAuthorName checks if the string IS an author name (stricter than isAuthorish).
// Used for reverse pattern detection (Title - Author).
func isAuthorName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	words := strings.Fields(s)
	if len(words) < 1 || len(words) > 4 {
		return false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "the ") || strings.HasPrefix(lower, "a ") || strings.HasPrefix(lower, "an ") {
		return false
	}
	// Reject strings containing digits (not author names)
	for _, w := range words {
		for _, c := range w {
			if c >= '0' && c <= '9' {
				return false
			}
		}
	}
	titleWordCount := 0
	for _, w := range words {
		lw := strings.ToLower(w)
		if titleWords[lw] {
			titleWordCount++
		}
	}
	if len(words) > 2 && float64(titleWordCount)/float64(len(words)) > 0.3 {
		return false
	}
	for _, w := range words {
		if len(w) > 15 {
			return false
		}
	}
	return true
}

// isTitleLike checks if a string looks like a book title (not an author).
// Used to prevent treating book titles as author names.
func isTitleLike(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	// Titles often start with articles
	if strings.HasPrefix(lower, "the ") || strings.HasPrefix(lower, "a ") || strings.HasPrefix(lower, "an ") {
		return true
	}
	// Common title keywords
	titleKeywords := []string{
		"chronicles", "saga", "trilogy", "cycle", "series",
		"of ", "and ", "in ", "on ", "at ", "the ",
	}
	keywordCount := 0
	for _, kw := range titleKeywords {
		if strings.Contains(lower, kw) {
			keywordCount++
		}
	}
	words := strings.Fields(s)
	// Multi-word strings with many small words are likely titles
	if len(words) >= 4 {
		return true
	}
	// 3-word strings with at least one preposition/article likely title
	if len(words) == 3 && keywordCount >= 1 {
		return true
	}
	return keywordCount >= 2
}

// stripAllParens removes all parenthetical groups from a string.
func stripAllParens(s string) string {
	for {
		start := strings.Index(s, "(")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], ")")
		if end < 0 {
			break
		}
		end += start
		s = strings.TrimSpace(s[:start] + s[end+1:])
	}
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// StripExt removes the file extension from a path.
func StripExt(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Exported checks for use by the organizer.
// IsAuthorLike returns true if the string looks like an author name.
func IsAuthorLike(s string) bool { return isAuthorName(s) }

// IsTitleLike returns true if the string looks like a book title.
func IsTitleLike(s string) bool { return isTitleLike(s) }

// IsAuthorish returns true if the string might be an author name (loose check).
func IsAuthorish(s string) bool { return isAuthorish(s) }
