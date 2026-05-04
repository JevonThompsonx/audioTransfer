// Package metadata provides Open Library API enrichment for audiobook metadata.
package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

const openLibrarySearchURL = "https://openlibrary.org/search.json"

var (
	httpClient = &http.Client{Timeout: 15 * time.Second}
	cache      = make(map[string]*cachedEntry)
	cacheMu    sync.RWMutex
)

type cachedEntry struct {
	metadata   *models.BookMetadata
	expiresAt  time.Time
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
	cacheKey := buildCacheKey(title, author)

	// Check cache first
	cacheMu.RLock()
	if entry, ok := cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		cacheMu.RUnlock()
		utils.Debug.Printf("Cache hit for '%s'", cacheKey)
		return entry.metadata
	}
	cacheMu.RUnlock()

	result := searchOpenLibrary(title, author)

	// Store in cache
	cacheMu.Lock()
	cache[cacheKey] = &cachedEntry{
		metadata:  result,
		expiresAt: time.Now().Add(1 * time.Hour),
	}
	cacheMu.Unlock()

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
