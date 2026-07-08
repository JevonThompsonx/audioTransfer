// Package metadata provides Open Library API enrichment for audiobook metadata.
package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

const openLibrarySearchURL = "https://openlibrary.org/search.json"

var (
	httpClient  = &http.Client{Timeout: 15 * time.Second}
	cache       = make(map[string]*cachedEntry)
	cacheMu     sync.RWMutex
	cacheOnce   sync.Once
	cacheLoadErr error
)

type cachedEntry struct {
	Metadata  *models.BookMetadata `json:"metadata"`
	ExpiresAt time.Time            `json:"expiresAt"`
}

type olSearchResponse struct {
	Docs []olDoc `json:"docs"`
}

type olDoc struct {
	Title        string   `json:"title"`
	AuthorName   []string `json:"author_name"`
	FirstPublishYear int  `json:"first_publish_year"`
	Key          string   `json:"key"`
	AuthorKey    []string `json:"author_key"`
	CoverI       int      `json:"cover_i"`
}

// Lookup searches Open Library for book metadata.
// Returns nil if nothing is found.
func Lookup(title string, author string) *models.BookMetadata {
	// Load cache from disk once on first call
	cacheOnce.Do(func() {
		cacheLoadErr = loadCacheFromDisk()
	})

	cacheKey := buildCacheKey(title, author)

	// Check cache first
	cacheMu.RLock()
	if entry, ok := cache[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		cacheMu.RUnlock()
		utils.Debug.Printf("Cache hit for '%s'", cacheKey)
		return entry.Metadata
	}
	cacheMu.RUnlock()

	result := searchOpenLibrary(title, author)

	// Store in cache
	cacheMu.Lock()
	cache[cacheKey] = &cachedEntry{
		Metadata:  result,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour), // 30 days
	}
	cacheMu.Unlock()

	// Persist cache to disk only on cache miss with a real result
	if result != nil {
		_ = saveCacheToDisk()
	}

	return result
}

func searchOpenLibrary(title string, author string) *models.BookMetadata {
	params := url.Values{}
	query := strings.TrimSpace(title)
	if author != "" {
		query += " " + author
	}
	params.Set("q", query)
	params.Set("limit", "3")
	params.Set("fields", "title,author_name,first_publish_year,key,author_key,cover_i")

	reqURL := fmt.Sprintf("%s?%s", openLibrarySearchURL, params.Encode())
	utils.Debug.Printf("OpenLibrary API: %s", reqURL)

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		utils.Warn.Printf("OpenLibrary request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		utils.Warn.Printf("OpenLibrary returned %d", resp.StatusCode)
		return nil
	}

	var sr olSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		utils.Warn.Printf("OpenLibrary parse failed: %v", err)
		return nil
	}

	if len(sr.Docs) == 0 {
		return nil
	}

	doc := sr.Docs[0]

	meta := &models.BookMetadata{
		Title:     doc.Title,
		Author:    strings.Join(doc.AuthorName, ", "),
		Year:      doc.FirstPublishYear,
		OLWorkKey: doc.Key,
		Source:    "openlibrary",
	}

	if len(doc.AuthorKey) > 0 {
		meta.OLAuthorKey = doc.AuthorKey[0]
	}
	if doc.CoverI > 0 {
		meta.CoverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", doc.CoverI)
	}

	utils.Debug.Printf("Found: %s by %s (%d)", meta.Title, meta.Author, meta.Year)
	return meta
}

func buildCacheKey(title, author string) string {
	return fmt.Sprintf("%s|%s", strings.ToLower(strings.TrimSpace(title)),
		strings.ToLower(strings.TrimSpace(author)))
}

func loadCacheFromDisk() error {
	configDir, err := utils.ConfigDir()
	if err != nil {
		utils.Debug.Printf("Unable to load metadata cache: %v", err)
		return nil // Don't fail lookup if we can't determine config dir
	}

	cacheFile := filepath.Join(configDir, "metadata_cache.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			utils.Debug.Printf("Starting with empty metadata cache")
			return nil // File doesn't exist yet, that's normal
		}
		utils.Debug.Printf("Failed to read metadata cache file: %v", err)
		return nil // Corrupt cache is not a fatal error
	}

	var loaded map[string]*cachedEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		utils.Debug.Printf("Failed to parse metadata cache: %v", err)
		return nil // Corrupt JSON, start fresh
	}

	// Filter out expired entries
	now := time.Now()
	cacheMu.Lock()
	for key, entry := range loaded {
		if now.Before(entry.ExpiresAt) {
			cache[key] = entry
		}
	}
	cacheMu.Unlock()

	utils.Debug.Printf("Loaded metadata cache with %d entries", len(cache))
	return nil
}

func saveCacheToDisk() error {
	configDir, err := utils.ConfigDir()
	if err != nil {
		utils.Debug.Printf("Unable to save metadata cache: %v", err)
		return nil // Don't fail if we can't determine config dir
	}

	cacheFile := filepath.Join(configDir, "metadata_cache.json")

	cacheMu.RLock()
	data, err := json.MarshalIndent(cache, "", "  ")
	cacheMu.RUnlock()

	if err != nil {
		utils.Debug.Printf("Failed to marshal metadata cache: %v", err)
		return err
	}

	if err := os.WriteFile(cacheFile, data, 0600); err != nil {
		utils.Debug.Printf("Failed to write metadata cache: %v", err)
		return err
	}

	return nil
}
