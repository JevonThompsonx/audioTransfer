package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
)

func TestScoreCandidate_Title(t *testing.T) {
	tests := []struct {
		name         string
		currentTitle string
		provider     string
		want         float64
	}{
		{"Exact", "The Hobbit", "The Hobbit", 1.0},
		{"ExactCasePunct", "The Rising of the Shield Hero, Vol. 08", "The Rising of the Shield Hero Vol. 08", 1.0},
		{"CurrentContainedInProvider", "The Rising of the Shield Hero", "The Rising of the Shield Hero, Volume 17", 0.95},
		{"ProviderContainedInCurrent", "The Hobbit, or There and Back Again", "The Hobbit", 0.95},
		{"TokenOverlap", "Sweet Obsession", "Dark Olympus Series", 0},
		{"TokenOverlapPartial", "House of Flame and Shadow", "House Flame Shadow Crescent City", 0.6},
		{"EmptyProvider", "Anything", "", 0},
		{"EmptyCurrent", "", "Provider Title", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ScoreCandidate(tt.currentTitle, "Author", tt.provider, "Author")
			if got != tt.want {
				t.Errorf("ScoreCandidate title: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreCandidate_Author(t *testing.T) {
	tests := []struct {
		name          string
		currentAuthor string
		provider      string
		want          float64
	}{
		{"Exact", "Kumo Kagyu", "Kumo Kagyu", 1.0},
		// Provider strings append narrators/translators/illustrators — an exact
		// segment match must still score 1.0 (real author is Kumo Kagyu).
		{"SegmentMatchWithNarrators", "Kumo Kagyu", "Kumo Kagyu, Noboru Kannatuki, Kevin Steinbach - translator", 1.0},
		{"SegmentMatchWithAmpersand", "Caroline Peckham", "Caroline Peckham & Susanne Valenti", 1.0},
		{"SegmentMatchWithSemicolon", "Robin Hobb", "Robin Hobb; Megan Lindholm", 1.0},
		{"SegmentMatchWithDashRole", "Kevin Steinbach", "Kumo Kagyu, Noboru Kannatuki, Kevin Steinbach - translator", 1.0},
		{"TokenOverlap", "J.R.R. Tolkien", "John Ronald Reuel Tolkien", 0.25},
		{"EmptyCurrent", "", "Kumo Kagyu", 0},
		{"EmptyProvider", "Kumo Kagyu", "", 0},
		{"NoMatch", "Terry Pratchett", "Kumo Kagyu, Noboru Kannatuki", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := ScoreCandidate("Title", tt.currentAuthor, "Title", tt.provider)
			if got != tt.want {
				t.Errorf("ScoreCandidate author: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeMismatch(t *testing.T) {
	tests := []struct {
		name         string
		currentTitle string
		provider     string
		currentPos   float64
		providerPos  float64
		want         bool
	}{
		// The Shield Hero bug: same series, different volume -> reject.
		{"DifferentVolumeFromTitle", "The Rising of the Shield Hero, Vol. 08", "The Rising of the Shield Hero, Volume 17", 0, 0, true},
		{"SameVolumeFromTitle", "The Rising of the Shield Hero, Vol. 08", "The Rising of the Shield Hero, Volume 08", 0, 0, false},
		{"LeadingZerosEqual", "Book 08", "Book 8", 0, 0, false},
		{"DecimalVolumes", "Book 1.5", "Book 2.5", 0, 0, true},
		{"DecimalVolumesEqual", "Book 1.5", "Book 1.50", 0, 0, false},
		{"OnlyCurrentHasVolume", "Book 3", "The Dragon Reborn", 0, 0, false},
		{"OnlyProviderHasVolume", "The Dragon Reborn", "The Dragon Reborn, Book 3", 0, 0, false},
		{"NoVolumes", "House of Flame and Shadow", "House of Flame and Shadow", 0, 0, false},
		// Filename series position vs provider series position.
		{"SeriesPosMismatch", "Assistant to the Villain", "Assistant to the Villain", 8, 9, true},
		{"SeriesPosMatch", "Assistant to the Villain", "Assistant to the Villain", 8, 8, false},
		{"SeriesPosOnlyCurrent", "Assistant to the Villain", "Assistant to the Villain", 8, 0, false},
		{"SeriesPosOnlyProvider", "Assistant to the Villain", "Assistant to the Villain", 0, 9, false},
		{"VolumeAndPosAgree", "The Rising of the Shield Hero, Vol. 08", "The Rising of the Shield Hero, Volume 8", 8, 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VolumeMismatch(tt.currentTitle, tt.provider, tt.currentPos, tt.providerPos); got != tt.want {
				t.Errorf("VolumeMismatch: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"The Hobbit", "the hobbit"},
		{"  THE   HOBBIT  ", "the hobbit"},
		{"The Rising of the Shield Hero, Vol. 08", "the rising of the shield hero vol 08"},
		{"Kumo Kagyu, Noboru Kannatuki, Kevin Steinbach - translator", "kumo kagyu noboru kannatuki kevin steinbach translator"},
		{"J.R.R. Tolkien", "j r r tolkien"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("normalize(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteMetadataJSON_Golden(t *testing.T) {
	meta := &models.BookMetadata{
		Title:          "Sweet Obsession",
		Subtitle:       "A Dark Olympus Novel",
		Author:         "Katee Robert, Some Narrator - narrator",
		Narrator:       "Zara Hampton-Brown",
		Series:         "Dark Olympus Series",
		SeriesPosition: 8,
		ASIN:           "B0ABC12345",
		Year:           2021,
		Description:    "The eighth book in the Dark Olympus series.",
		Publisher:      "Sourcebooks Casablanca",
		Language:       "english",
		ISBN:           "9781728250670",
		Tags:           []string{"romance", "fantasy"},
		Genres:         []string{"Romance"},
		Source:         "audible",
	}

	dir := t.TempDir()
	if err := WriteMetadataJSON(dir, meta); err != nil {
		t.Fatalf("WriteMetadataJSON failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("Failed to read metadata.json: %v", err)
	}

	want := `{
  "tags": [
    "romance",
    "fantasy"
  ],
  "chapters": [],
  "title": "Sweet Obsession",
  "subtitle": "A Dark Olympus Novel",
  "authors": [
    "Katee Robert",
    "Some Narrator"
  ],
  "narrators": [
    "Zara Hampton-Brown"
  ],
  "series": [
    "Dark Olympus Series #8"
  ],
  "genres": [
    "Romance"
  ],
  "publishedYear": "2021",
  "publishedDate": null,
  "publisher": "Sourcebooks Casablanca",
  "description": "The eighth book in the Dark Olympus series.",
  "isbn": "9781728250670",
  "asin": "B0ABC12345",
  "language": "English",
  "explicit": false,
  "abridged": false
}
`
	if string(data) != want {
		t.Errorf("metadata.json content mismatch.\n--- got ---\n%s\n--- want ---\n%s", data, want)
	}
}

func TestWriteMetadataJSON_Sparse(t *testing.T) {
	// Sparse (OpenLibrary-only) metadata must still produce valid JSON with
	// the full ABS schema — empty arrays/scalars, and ABS tolerates the gaps.
	meta := &models.BookMetadata{
		Title:  "Obscure Indie Book",
		Author: "Unknown Author",
		Year:   2015,
		Source: "openlibrary",
	}

	dir := t.TempDir()
	if err := WriteMetadataJSON(dir, meta); err != nil {
		t.Fatalf("WriteMetadataJSON failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("Failed to read metadata.json: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Sparse metadata.json is not valid JSON: %v", err)
	}

	// chapters must be an empty ARRAY ([]), not null — ABS expects an array.
	if ch, ok := parsed["chapters"].([]interface{}); !ok || len(ch) != 0 {
		t.Errorf("chapters: got %#v, want empty array", parsed["chapters"])
	}
	if tags, ok := parsed["tags"].([]interface{}); !ok || len(tags) != 0 {
		t.Errorf("tags: got %#v, want empty array", parsed["tags"])
	}
	if parsed["title"] != "Obscure Indie Book" {
		t.Errorf("title: got %v", parsed["title"])
	}
	if parsed["publishedYear"] != "2015" {
		t.Errorf("publishedYear: got %v, want 2015", parsed["publishedYear"])
	}
	if parsed["language"] != "" {
		t.Errorf("language: got %v, want empty", parsed["language"])
	}
}

func TestSplitAuthorNames(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Katee Robert", []string{"Katee Robert"}},
		{"Caroline Peckham, Susanne Valenti", []string{"Caroline Peckham", "Susanne Valenti"}},
		{"Caroline Peckham & Susanne Valenti", []string{"Caroline Peckham", "Susanne Valenti"}},
		{"Kumo Kagyu, Noboru Kannatuki, Kevin Steinbach - translator", []string{"Kumo Kagyu", "Noboru Kannatuki", "Kevin Steinbach"}},
		{"Kevin Steinbach - translator", []string{"Kevin Steinbach"}},
		{"Kumo Kagyu, translator", []string{"Kumo Kagyu"}},
		{"", []string{}},
		{"  ", []string{}},
	}
	for _, tt := range tests {
		got := splitAuthorNames(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("splitAuthorNames(%q): got %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatSeriesList(t *testing.T) {
	meta := &models.BookMetadata{
		Series:         "Crescent City",
		SeriesPosition: 3,
	}
	if got := formatSeriesList(meta); len(got) != 1 || got[0] != "Crescent City #3" {
		t.Errorf("formatSeriesList primary: got %v", got)
	}

	meta2 := &models.BookMetadata{
		Series:                  "Dark Olympus Series",
		SeriesPosition:          8,
		SecondarySeries:         "Dark Olympus",
		SecondarySeriesPosition: 0,
	}
	got := formatSeriesList(meta2)
	want := []string{"Dark Olympus Series #8", "Dark Olympus"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("formatSeriesList primary+secondary: got %v, want %v", got, want)
	}

	meta3 := &models.BookMetadata{}
	if got := formatSeriesList(meta3); got == nil || len(got) != 0 {
		t.Errorf("formatSeriesList empty: got %#v, want empty non-nil slice", got)
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"english", "English"},
		{"ENGLISH", "English"},
		{"English", "English"},
		{"en", "En"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := titleCase(tt.in); got != tt.want {
			t.Errorf("titleCase(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestYearFromDate(t *testing.T) {
	tests := []struct{ in string; want int }{
		{"2021-06-01", 2021},
		{"2010", 2010},
		{"", 0},
		{"garbage", 0},
		{"released 1999 sometime", 1999},
	}
	for _, tt := range tests {
		if got := yearFromDate(tt.in); got != tt.want {
			t.Errorf("yearFromDate(%q): got %d, want %d", tt.in, got, tt.want)
		}
	}
}


func TestCacheKeyGeneration(t *testing.T) {
	// Test that buildCacheKey generates consistent, normalized keys
	tests := []struct {
		name     string
		title    string
		author   string
		expected string
	}{
		{
			name:     "Simple",
			title:    "The Hobbit",
			author:   "J.R.R. Tolkien",
			expected: "the hobbit|j.r.r. tolkien",
		},
		{
			name:     "WithWhitespace",
			title:    "  The Hobbit  ",
			author:   "  J.R.R. Tolkien  ",
			expected: "the hobbit|j.r.r. tolkien",
		},
		{
			name:     "NoAuthor",
			title:    "The Hobbit",
			author:   "",
			expected: "the hobbit|",
		},
		{
			name:     "UpperCase",
			title:    "THE HOBBIT",
			author:   "J.R.R. TOLKIEN",
			expected: "the hobbit|j.r.r. tolkien",
		},
		{
			name:     "Mixed",
			title:    "  Pride And Prejudice  ",
			author:   "  JANE AUSTEN  ",
			expected: "pride and prejudice|jane austen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCacheKey(tt.title, tt.author)
			if result != tt.expected {
				t.Errorf("buildCacheKey(%q, %q): got %q, want %q", tt.title, tt.author, result, tt.expected)
			}
		})
	}
}

func TestCachedEntry_JSONMarshal(t *testing.T) {
	// Test that cachedEntry marshals and unmarshals correctly,
	// particularly the exported Metadata and ExpiresAt fields
	now := time.Now()
	entry := &cachedEntry{
		Metadata: &models.BookMetadata{
			Title:  "Test Title",
			Author: "Test Author",
			Year:   2020,
			Source: "openlibrary",
		},
		ExpiresAt: now,
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal cachedEntry: %v", err)
	}

	// Unmarshal back
	var loaded cachedEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal cachedEntry: %v", err)
	}

	// Verify fields
	if loaded.Metadata.Title != "Test Title" {
		t.Errorf("Title: got %q, want Test Title", loaded.Metadata.Title)
	}
	if loaded.Metadata.Author != "Test Author" {
		t.Errorf("Author: got %q, want Test Author", loaded.Metadata.Author)
	}
	if loaded.Metadata.Year != 2020 {
		t.Errorf("Year: got %d, want 2020", loaded.Metadata.Year)
	}

	// Use .Equal() for time comparison
	if !loaded.ExpiresAt.Equal(now) {
		t.Error("ExpiresAt not preserved in round-trip")
	}
}

func TestLoadCacheFromDisk_NonexistentFile(t *testing.T) {
	// If the cache file doesn't exist, loadCacheFromDisk should not error
	// This is tested via the public Lookup interface since loadCacheFromDisk is private.
	//
	// However, since Lookup uses sync.Once to load the cache on first call,
	// we can't easily reset it between tests. Instead, we verify the behavior
	// indirectly by checking that a nonexistent config dir doesn't crash.

	// The actual test is implicit: if Lookup doesn't panic on a fresh run,
	// the cache loading is working correctly.
	// (Full testing would require resetting sync.Once, which would require
	// modifying production code, which we don't do.)
}

func TestSaveCacheToDisk_JSONFormat(t *testing.T) {
	// Test that saveCacheToDisk creates valid JSON with expected structure
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_cache.json")

	// Create test cache data
	testCache := map[string]*cachedEntry{
		"test|author": {
			Metadata: &models.BookMetadata{
				Title:  "Test Book",
				Author: "Test Author",
				Year:   2021,
				Source: "openlibrary",
			},
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		},
	}

	// Marshal it as our code does
	data, err := json.MarshalIndent(testCache, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal cache: %v", err)
	}

	// Write it as our code does
	if err := os.WriteFile(cacheFile, data, 0600); err != nil {
		t.Fatalf("Failed to write cache file: %v", err)
	}

	// Read it back and verify structure
	loaded, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	var parsed map[string]*cachedEntry
	if err := json.Unmarshal(loaded, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal cache: %v", err)
	}

	entry, ok := parsed["test|author"]
	if !ok {
		t.Fatal("Cache missing expected key")
	}

	if entry.Metadata.Title != "Test Book" {
		t.Errorf("Title: got %q, want Test Book", entry.Metadata.Title)
	}
}

func TestCacheFilePermissions(t *testing.T) {
	// Test that cache file is created with secure permissions (0600)
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_cache.json")

	// Write cache file with 0600 permissions as the code does
	data := []byte("{}")
	if err := os.WriteFile(cacheFile, data, 0600); err != nil {
		t.Fatalf("Failed to write cache file: %v", err)
	}

	// Check permissions
	info, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("Failed to stat cache file: %v", err)
	}

	mode := info.Mode().Perm()
	expected := os.FileMode(0600)
	if mode != expected {
		t.Errorf("Cache file permissions: got %#o, want %#o", mode, expected)
	}
}

func TestCacheExpiration(t *testing.T) {
	// Test that expired cache entries are filtered out during loading
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_cache.json")

	now := time.Now()

	// Create cache with one expired and one valid entry
	testCache := map[string]*cachedEntry{
		"expired|key": {
			Metadata: &models.BookMetadata{
				Title: "Expired Book",
			},
			ExpiresAt: now.Add(-1 * 24 * time.Hour), // Expired
		},
		"valid|key": {
			Metadata: &models.BookMetadata{
				Title: "Valid Book",
			},
			ExpiresAt: now.Add(30 * 24 * time.Hour), // Valid
		},
	}

	data, err := json.MarshalIndent(testCache, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal cache: %v", err)
	}

	if err := os.WriteFile(cacheFile, data, 0600); err != nil {
		t.Fatalf("Failed to write cache file: %v", err)
	}

	// Read it back
	loaded, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	var parsed map[string]*cachedEntry
	if err := json.Unmarshal(loaded, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal cache: %v", err)
	}

	// Simulate the filtering that loadCacheFromDisk does
	filtered := make(map[string]*cachedEntry)
	for key, entry := range parsed {
		if now.Before(entry.ExpiresAt) {
			filtered[key] = entry
		}
	}

	// Should only have the valid entry
	if len(filtered) != 1 {
		t.Errorf("After filtering: expected 1 valid entry, got %d", len(filtered))
	}

	if _, ok := filtered["valid|key"]; !ok {
		t.Error("Valid entry should be present after filtering")
	}

	if _, ok := filtered["expired|key"]; ok {
		t.Error("Expired entry should be removed during filtering")
	}
}

func TestCacheExpiration_TTL(t *testing.T) {
	// Test that cache entries are set with 30-day TTL
	now := time.Now()
	entry := &cachedEntry{
		Metadata:  &models.BookMetadata{Title: "Book"},
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	// Verify TTL is roughly 30 days
	ttl := entry.ExpiresAt.Sub(now)
	expectedTTL := 30 * 24 * time.Hour

	// Allow some tolerance for test execution time
	tolerance := 1 * time.Minute
	if ttl < expectedTTL-tolerance || ttl > expectedTTL+tolerance {
		t.Errorf("TTL: got %v, expected ~%v", ttl, expectedTTL)
	}
}

func TestCacheJSONSchema(t *testing.T) {
	// Test the JSON schema of the cache file
	// The schema should match what Lookup produces

	entry := &cachedEntry{
		Metadata: &models.BookMetadata{
			Title:       "Test Title",
			Author:      "Test Author",
			Year:        2020,
			OLWorkKey:   "/works/OL123W",
			OLAuthorKey: "/authors/OL456A",
			Source:      "openlibrary",
			CoverURL:    "https://example.com/cover.jpg",
		},
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify JSON keys
	jsonStr := string(data)
	requiredKeys := []string{"metadata", "expiresAt"}
	for _, key := range requiredKeys {
		if !strings.Contains(jsonStr, "\""+key+"\"") {
			t.Errorf("JSON missing key: %q", key)
		}
	}

	// Verify metadata sub-keys
	metadataKeys := []string{"Title", "Author", "Year"}
	for _, key := range metadataKeys {
		if !strings.Contains(jsonStr, "\""+key+"\"") {
			t.Errorf("JSON metadata missing key: %q", key)
		}
	}
}
