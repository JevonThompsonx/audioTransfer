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

	// Title [ASIN]
	{regexp.MustCompile(`^(.+?)\s*\[([A-Z0-9]{10})\]$`),
		map[string]int{"title": 1, "asin": 2, "_conf": 65}},

	// Author - Title (standard, lower confidence since it catches everything)
	{regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+)$`),
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

	// Look for series keywords in parent name
	keywordRe := regexp.MustCompile(`(?i)\s+(Series|Saga|Trilogy|Cycle|Chronicles|books\s+\d+)`)
	keywordMatch := keywordRe.FindStringIndex(parentName)

	if keywordMatch != nil && info.Series == "" {
		before := strings.TrimSpace(parentName[:keywordMatch[0]])
		words := strings.Fields(before)

		if len(words) > 0 {
			var seriesWords []string

			if info.Author == "" && len(words) >= 3 {
				potentialAuthor := words[0] + " " + words[1]
				info.Author = potentialAuthor
				if info.Confidence < 35 {
					info.Confidence = 35
				}
				seriesWords = words[2:]
			} else if info.Author != "" {
				authorWords := strings.Fields(info.Author)
				if len(authorWords) < len(words) {
					seriesWords = words[len(authorWords):]
				}
			} else {
				if len(words) > 4 {
					seriesWords = words[len(words)-4:]
				} else {
					seriesWords = words
				}
			}

			if len(seriesWords) > 0 {
				info.Series = strings.Join(seriesWords, " ")
				if info.Confidence < 40 {
					info.Confidence = 40
				}
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
		if len(w) > 12 {
			return false
		}
	}
	return true
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
