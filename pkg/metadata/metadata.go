// Package metadata provides multi-provider API enrichment for audiobook metadata.
//
// Provider chain (first confident hit wins):
//
//  1. Audible catalog search (https://api.audible.com/1.0/catalog/products) —
//     returns candidate ASINs, which are then enriched per-ASIN via the
//     audnex.us community proxy (https://api.audnex.us/books/<ASIN>). This is
//     the same source Audiobookshelf itself uses and yields the richest data:
//     title, subtitle, authors, narrators, publisher, summary, release date,
//     cover, genres/tags, primary+secondary series with positions, language,
//     ISBN and ASIN.
//  2. iTunes search (https://itunes.apple.com/search, media=audiobook) —
//     description-only fallback (no tags/series), 100% hit rate in practice.
//  3. OpenLibrary search (https://openlibrary.org/search.json) — last-resort,
//     sparse fallback (title/author/year/cover only).
//
// Every provider result is scored against the current title/author with a
// volume-aware matcher: naive top-result matching is rejected when the result
// is a different volume of the same series (e.g. matching "Vol. 08" to
// "Volume 17" of the same series). Results are cached persistently
// (~/.audiotransfer/metadata_cache.json, 30-day TTL).
package metadata

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jevonx/audioTransfer/pkg/models"
	"github.com/jevonx/audioTransfer/pkg/utils"
)

const (
	openLibrarySearchURL = "https://openlibrary.org/search.json"
	audibleSearchURL     = "https://api.audible.com/1.0/catalog/products"
	audnexBookURL        = "https://api.audnex.us/books/"
	itunesSearchURL      = "https://itunes.apple.com/search"

	// maxAudnexWorkers bounds concurrent per-ASIN enrichment requests.
	maxAudnexWorkers = 4

	// Acceptance thresholds: title must be decent and the author match must
	// be strong (author_score >= 0.66). Loose title + strict author avoids
	// matching the wrong volume/edition.
	minTitleScore  = 0.6
	minAuthorScore = 0.66
)

var (
	httpClient   = &http.Client{Timeout: 10 * time.Second}
	cache        = make(map[string]*cachedEntry)
	cacheMu      sync.RWMutex
	cacheOnce    sync.Once
	cacheLoadErr error
)

type cachedEntry struct {
	Metadata  *models.BookMetadata `json:"metadata"`
	ExpiresAt time.Time            `json:"expiresAt"`
}

// lookupQuery carries the current book identity through the provider chain.
type lookupQuery struct {
	title     string
	author    string
	series    string
	seriesPos float64
}

// --- Public API ---

// Lookup searches the provider chain for book metadata using only the
// title/author (no series context). Returns nil when nothing is found
// confidently. Volume-awareness is limited to title-embedded markers.
func Lookup(title string, author string) *models.BookMetadata {
	return lookupWithCache(lookupQuery{title: title, author: author})
}

// LookupEnriched searches the provider chain with full series context. The
// parsed series position from the filename is used to reject provider results
// that belong to a different volume of the series (the Shield Hero bug).
func LookupEnriched(title, author, series string, seriesPos float64) *models.BookMetadata {
	return lookupWithCache(lookupQuery{title: title, author: author, series: series, seriesPos: seriesPos})
}

func lookupWithCache(q lookupQuery) *models.BookMetadata {
	// Load cache from disk once on first call
	cacheOnce.Do(func() {
		cacheLoadErr = loadCacheFromDisk()
	})

	cacheKey := enrichedCacheKey(q)

	// Check cache first
	cacheMu.RLock()
	if entry, ok := cache[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		cacheMu.RUnlock()
		utils.Debug.Printf("Cache hit for '%s'", cacheKey)
		return entry.Metadata
	}
	cacheMu.RUnlock()

	var result *models.BookMetadata
	if m := lookupAudible(q); m != nil {
		result = m
	} else if m := lookupITunes(q); m != nil {
		result = m
	} else if m := lookupOpenLibrary(q); m != nil {
		result = m
	}

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

// enrichedCacheKey extends the plain title|author key with the parsed series
// context, so a volume-aware enrichment can never be served a stale
// volume-blind cache entry (and vice versa). Old-format entries simply expire.
func enrichedCacheKey(q lookupQuery) string {
	pos := ""
	if q.seriesPos > 0 {
		pos = strconv.FormatFloat(q.seriesPos, 'f', -1, 64)
	}
	return fmt.Sprintf("%s|%s|%s|%s",
		strings.ToLower(strings.TrimSpace(q.title)),
		strings.ToLower(strings.TrimSpace(q.author)),
		strings.ToLower(strings.TrimSpace(q.series)),
		pos)
}

// --- Audible chain: catalog search -> per-ASIN audnex enrichment ---

type audibleCatalogResponse struct {
	Products []audibleCatalogProduct `json:"products"`
}

type audibleCatalogProduct struct {
	ASIN    string         `json:"asin"`
	Title   string         `json:"title"`
	Authors []audnexAuthor `json:"authors"`
}

type audnexSeries struct {
	Name     string         `json:"name"`
	Position flexibleNumber `json:"position"`
}

// flexibleNumber accepts both JSON numbers and numeric strings (audnex emits
// series positions as strings, e.g. "position": "8").
type flexibleNumber float64

func (f *flexibleNumber) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexibleNumber(v)
	return nil
}

type audnexGenre struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type audnexAuthor struct {
	Name string `json:"name"`
	ASIN string `json:"asin"`
}

type audnexBook struct {
	ASIN             string         `json:"asin"`
	Title            string         `json:"title"`
	Subtitle         string         `json:"subtitle"`
	Authors          []audnexAuthor `json:"authors"`
	Narrators        []audnexAuthor `json:"narrators"`
	PublisherName    string         `json:"publisherName"`
	Summary          string         `json:"summary"`
	ReleaseDate      string         `json:"releaseDate"`
	Image            string         `json:"image"`
	Genres           []audnexGenre  `json:"genres"`
	SeriesPrimary    *audnexSeries  `json:"seriesPrimary"`
	SeriesSecondary  *audnexSeries  `json:"seriesSecondary"`
	Language         string         `json:"language"`
	ISBN             string         `json:"isbn"`
	RuntimeLengthMin int            `json:"runtimeLengthMin"`
}

func lookupAudible(q lookupQuery) *models.BookMetadata {
	products := audibleCatalogSearch(q)
	if len(products) == 0 {
		return nil
	}

	// Enrich each candidate ASIN in parallel (bounded worker pool). audnex is
	// a free community proxy — a failing ASIN must not sink the whole search.
	asins := make([]string, 0, len(products))
	for _, p := range products {
		if p.ASIN != "" {
			asins = append(asins, p.ASIN)
		}
	}
	candidates := enrichASINs(asins)

	best := pickBestCandidate(q, candidates)

	// Fallback: audnex enrichment failed for every ASIN (or nothing passed the
	// match thresholds) — score the catalog-level title/author instead.
	if best == nil {
		fallbacks := make([]*models.BookMetadata, 0, len(products))
		for _, p := range products {
			if p.Title == "" {
				continue
			}
			fallbacks = append(fallbacks, &models.BookMetadata{
				Title:  p.Title,
				Author: joinNames(p.Authors),
				Source: "audible",
			})
		}
		best = pickBestCandidate(q, fallbacks)
	}

	if best != nil {
		best.Confidence = 80
		if best.Source == "" {
			best.Source = "audible"
		}
		utils.Debug.Printf("Audible matched: %s by %s (asin=%s)", best.Title, best.Author, best.ASIN)
	}
	return best
}

// pickBestCandidate scores every candidate and returns the highest-scoring one
// that passes the acceptance thresholds and the volume check.
func pickBestCandidate(q lookupQuery, candidates []*models.BookMetadata) *models.BookMetadata {
	var best *models.BookMetadata
	var bestTitle, bestAuthor float64
	for _, c := range candidates {
		if c == nil {
			continue
		}
		ts, as := ScoreCandidate(q.title, q.author, c.Title, c.Author)
		if VolumeMismatch(q.title, c.Title, q.seriesPos, c.SeriesPosition) {
			utils.Debug.Printf("Rejected (volume mismatch): %q vs %q", q.title, c.Title)
			continue
		}
		if ts < minTitleScore || as < minAuthorScore {
			continue
		}
		if best == nil || ts > bestTitle || (ts == bestTitle && as > bestAuthor) {
			best = c
			bestTitle, bestAuthor = ts, as
		}
	}
	return best
}

// enrichASINs fetches audnex enrichment for each ASIN concurrently (max 4).
func enrichASINs(asins []string) []*models.BookMetadata {
	results := make([]*models.BookMetadata, len(asins))
	if len(asins) == 0 {
		return results
	}
	sem := make(chan struct{}, maxAudnexWorkers)
	var wg sync.WaitGroup
	for i, asin := range asins {
		wg.Add(1)
		go func(i int, asin string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if m := enrichAudnex(asin); m != nil {
				results[i] = m
			}
		}(i, asin)
	}
	wg.Wait()
	return results
}

func enrichAudnex(asin string) *models.BookMetadata {
	reqURL := audnexBookURL + url.PathEscape(asin)
	utils.Debug.Printf("audnex API: %s", reqURL)

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		utils.Warn.Printf("audnex request failed for %s: %v", asin, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		utils.Warn.Printf("audnex returned %d for %s", resp.StatusCode, asin)
		return nil
	}

	var b audnexBook
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		utils.Warn.Printf("audnex parse failed for %s: %v", asin, err)
		return nil
	}
	if b.Title == "" {
		utils.Debug.Printf("audnex returned empty title for %s", asin)
		return nil
	}

	meta := &models.BookMetadata{
		Title:       b.Title,
		Author:      joinNames(b.Authors),
		Narrator:    joinNames(b.Narrators),
		ASIN:        b.ASIN,
		Subtitle:    b.Subtitle,
		Publisher:   b.PublisherName,
		Description: b.Summary,
		CoverURL:    b.Image,
		Language:    b.Language,
		ISBN:        b.ISBN,
		Year:        yearFromDate(b.ReleaseDate),
		Source:      "audible",
	}
	if b.SeriesPrimary != nil {
		meta.Series = b.SeriesPrimary.Name
		meta.SeriesPosition = float64(b.SeriesPrimary.Position)
	}
	if b.SeriesSecondary != nil {
		meta.SecondarySeries = b.SeriesSecondary.Name
		meta.SecondarySeriesPosition = float64(b.SeriesSecondary.Position)
	}
	for _, g := range b.Genres {
		if g.Name == "" {
			continue
		}
		if g.Type == "tag" || g.Type == "" {
			meta.Tags = append(meta.Tags, g.Name)
		} else {
			meta.Genres = append(meta.Genres, g.Name)
		}
	}
	meta.Description = cleanDescription(b.Summary)
	return meta
}

// cleanDescription strips HTML tags from provider summaries (audnex returns
// <p>/<b> markup) and unescapes entities, so ABS shows readable plain text.
func cleanDescription(s string) string {
	if s == "" {
		return ""
	}
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// audibleCatalogSearch returns the catalog products for a title/author query.
// Only the ASINs (and best-effort title/author) from this response are used.
func audibleCatalogSearch(q lookupQuery) []audibleCatalogProduct {
	params := url.Values{}
	params.Set("num_results", "10")
	params.Set("products_sort_by", "Relevance")
	params.Set("title", searchTitle(q.title))
	if author := strings.TrimSpace(q.author); author != "" {
		params.Set("author", author)
	}
	reqURL := fmt.Sprintf("%s?%s", audibleSearchURL, params.Encode())
	utils.Debug.Printf("Audible catalog API: %s", reqURL)

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		utils.Warn.Printf("Audible catalog request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		utils.Warn.Printf("Audible catalog returned %d", resp.StatusCode)
		return nil
	}

	var cr audibleCatalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		utils.Warn.Printf("Audible catalog parse failed: %v", err)
		return nil
	}
	return cr.Products
}

// --- iTunes fallback (description only, no tags/series) ---

type itunesResponse struct {
	Results []struct {
		TrackName      string `json:"trackName"`
		ArtistName     string `json:"artistName"`
		Description    string `json:"description"`
		ReleaseDate    string `json:"releaseDate"`
		CollectionName string `json:"collectionName"`
	} `json:"results"`
}

// searchTitle strips volume markers ("Vol. 08", "Book 3") from a title before
// using it as a provider search query — search engines match better without
// them, and the volume check at match time still guards against wrong volumes.
func searchTitle(title string) string {
	t := volumeRe.ReplaceAllString(title, " ")
	t = strings.Trim(t, " ,;:-")
	return strings.TrimSpace(t)
}

func lookupITunes(q lookupQuery) *models.BookMetadata {
	params := url.Values{}
	term := searchTitle(q.title)
	if author := strings.TrimSpace(q.author); author != "" {
		term += " " + author
	}
	params.Set("term", term)
	params.Set("media", "audiobook")
	params.Set("limit", "25")

	reqURL := fmt.Sprintf("%s?%s", itunesSearchURL, params.Encode())
	utils.Debug.Printf("iTunes API: %s", reqURL)

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		utils.Warn.Printf("iTunes request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		utils.Warn.Printf("iTunes returned %d", resp.StatusCode)
		return nil
	}

	var ir itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		utils.Warn.Printf("iTunes parse failed: %v", err)
		return nil
	}

	var best *models.BookMetadata
	var bestTitle, bestAuthor float64
	for _, r := range ir.Results {
		// iTunes audiobook results carry the title in collectionName;
		// trackName is null for most audiobook listings.
		title := strings.TrimSpace(r.TrackName)
		if title == "" {
			title = strings.TrimSpace(r.CollectionName)
		}
		if title == "" {
			continue
		}
		ts, as := ScoreCandidate(q.title, q.author, title, r.ArtistName)
		if VolumeMismatch(q.title, title, q.seriesPos, 0) {
			continue
		}
		if ts < minTitleScore || as < minAuthorScore {
			continue
		}
		if best == nil || ts > bestTitle || (ts == bestTitle && as > bestAuthor) {
			best = &models.BookMetadata{
				Title:       title,
				Author:      r.ArtistName,
				Description: cleanDescription(r.Description),
				Year:        yearFromDate(r.ReleaseDate),
				Source:      "itunes",
				Confidence:  70,
			}
			bestTitle, bestAuthor = ts, as
		}
	}
	if best != nil {
		utils.Debug.Printf("iTunes matched: %s by %s", best.Title, best.Author)
	}
	return best
}

// --- OpenLibrary last-resort fallback ---

type olSearchResponse struct {
	Docs []olDoc `json:"docs"`
}

type olDoc struct {
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
	AuthorKey        []string `json:"author_key"`
	CoverI           int      `json:"cover_i"`
}

func lookupOpenLibrary(q lookupQuery) *models.BookMetadata {
	params := url.Values{}
	query := searchTitle(q.title)
	if q.author != "" {
		query += " " + q.author
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

	var best *models.BookMetadata
	var bestTitle, bestAuthor float64
	for _, doc := range sr.Docs {
		if doc.Title == "" {
			continue
		}
		author := strings.Join(doc.AuthorName, ", ")
		ts, as := ScoreCandidate(q.title, q.author, doc.Title, author)
		if VolumeMismatch(q.title, doc.Title, q.seriesPos, 0) {
			continue
		}
		if ts < minTitleScore || as < minAuthorScore {
			continue
		}
		if best == nil || ts > bestTitle || (ts == bestTitle && as > bestAuthor) {
			meta := &models.BookMetadata{
				Title:     doc.Title,
				Author:    author,
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
			best = meta
			bestTitle, bestAuthor = ts, as
		}
	}

	if best != nil {
		best.Confidence = 60
		utils.Debug.Printf("OpenLibrary matched: %s by %s (%d)", best.Title, best.Author, best.Year)
	}
	return best
}

// --- Match scoring ---

// ScoreCandidate scores a provider result against the current book identity.
// Returns the title score and author score, both in [0, 1]:
//
//	title:  exact = 1.0, containment either direction = 0.95, else
//	        token overlap = |intersection| / max(token counts).
//	author: normalized equality of the whole provider author string = 1.0,
//	        OR any comma/&/;-separated segment of the provider string exactly
//	        matching the current author = 1.0, else token overlap. An empty
//	        current author scores 0.
func ScoreCandidate(currentTitle, currentAuthor, providerTitle, providerAuthor string) (titleScore, authorScore float64) {
	titleScore = scoreNormalized(normalize(currentTitle), normalize(providerTitle))
	authorScore = computeAuthorScore(currentAuthor, providerAuthor)
	return
}

// VolumeMismatch reports whether the provider result is a different volume of
// the same series than the current book. Two independent checks:
//
//  1. If BOTH titles carry an explicit volume marker (Vol/Volume/Book/Bk
//     followed by a number, decimals supported) and the numbers differ
//     numerically (leading zeros stripped), the result is rejected.
//  2. If the parsed series position from the filename (currentPos) is set and
//     the provider's series position (providerPos) is set and differs, the
//     result is rejected. This is mandatory — matching "The Rising of the
//     Shield Hero, Vol. 08" to "Volume 17" of the same series is exactly the
//     bug this prevents.
func VolumeMismatch(currentTitle, providerTitle string, currentPos, providerPos float64) bool {
	cv, cok := extractVolume(currentTitle)
	pv, pok := extractVolume(providerTitle)
	if cok && pok && cv != pv {
		return true
	}
	if currentPos > 0 && providerPos > 0 && currentPos != providerPos {
		return true
	}
	return false
}

var volumeRe = regexp.MustCompile(`(?i)\b(?:vol(?:ume)?|book|bk)\.?\s*(\d+(?:\.\d+)?)\b`)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`) // strip HTML tags from summaries

func extractVolume(title string) (float64, bool) {
	m := volumeRe.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// normalize lowercases, strips non-alphanumeric characters (collapsing them
// to single spaces), and collapses whitespace.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func scoreNormalized(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a)) {
		return 0.95
	}
	return tokenOverlap(a, b)
}

func tokenOverlap(a, b string) float64 {
	ta, tb := strings.Fields(a), strings.Fields(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	set := make(map[string]bool, len(ta))
	for _, t := range ta {
		set[t] = true
	}
	inter := 0
	for _, t := range tb {
		if set[t] {
			inter++
		}
	}
	maxLen := len(ta)
	if len(tb) > maxLen {
		maxLen = len(tb)
	}
	return float64(inter) / float64(maxLen)
}

// computeAuthorScore compares the current author against a provider author string
// that may include narrators/translators/illustrators ("Kumo Kagyu, Noboru
// Kannatuki, Kevin Steinbach - translator"). A whole-string match scores 1.0;
// an exact normalized match on any comma/&/;-separated segment (with " - role"
// suffixes stripped) also scores 1.0.
func computeAuthorScore(currentAuthor, providerAuthor string) float64 {
	cur := normalize(currentAuthor)
	if cur == "" {
		return 0
	}
	prov := normalize(providerAuthor)
	if prov == "" {
		return 0
	}
	if cur == prov {
		return 1.0
	}
	for _, part := range splitAuthorSegments(providerAuthor) {
		if normalize(part) == cur {
			return 1.0
		}
	}
	return tokenOverlap(cur, prov)
}

func splitAuthorSegments(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '&' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		// Strip " - role" suffixes: "Kevin Steinbach - translator" -> "Kevin Steinbach"
		if idx := strings.Index(part, " - "); idx >= 0 {
			part = strings.TrimSpace(part[:idx])
		}
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// --- ABS metadata.json writer ---

// absMetadataFile mirrors the Audiobookshelf LibraryItem.saveMetadataFile
// schema (verified against ABS 2.36.0 source). ABS reads this file from the
// book's folder at scan time and treats it as authoritative metadata.
type absMetadataFile struct {
	Tags          []string `json:"tags"`
	Chapters      []string `json:"chapters"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle"`
	Authors       []string `json:"authors"`
	Narrators     []string `json:"narrators"`
	Series        []string `json:"series"`
	Genres        []string `json:"genres"`
	PublishedYear string   `json:"publishedYear"`
	PublishedDate *string  `json:"publishedDate"`
	Publisher     string   `json:"publisher"`
	Description   string   `json:"description"`
	ISBN          string   `json:"isbn"`
	ASIN          string   `json:"asin"`
	Language      string   `json:"language"`
	Explicit      bool     `json:"explicit"`
	Abridged      bool     `json:"abridged"`
}

// WriteMetadataJSON writes an ABS metadata.json file into dir. The file always
// carries the full ABS schema — arrays are emitted as [] and scalar fields as
// "" when the provider data is sparse (ABS tolerates missing fields; the file
// must simply be valid JSON).
func WriteMetadataJSON(dir string, meta *models.BookMetadata) error {
	if meta == nil {
		return fmt.Errorf("cannot write metadata.json: nil metadata")
	}
	f := &absMetadataFile{
		Tags:          stringSliceOrEmpty(meta.Tags),
		Chapters:      []string{},
		Title:         meta.Title,
		Subtitle:      meta.Subtitle,
		Authors:       splitAuthorNames(meta.Author),
		Narrators:     splitAuthorNames(meta.Narrator),
		Series:        formatSeriesList(meta),
		Genres:        stringSliceOrEmpty(meta.Genres),
		PublishedYear: formatYear(meta.Year),
		PublishedDate: nil,
		Publisher:     meta.Publisher,
		Description:   meta.Description,
		ISBN:          meta.ISBN,
		ASIN:          meta.ASIN,
		Language:      titleCase(meta.Language),
		Explicit:      false,
		Abridged:      false,
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0644)
}

func stringSliceOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// splitAuthorNames turns a possibly comma/&/;-joined author string into an
// array of individual author NAME STRINGS (dropping " - role" suffixes and
// pure role tokens like "translator").
func splitAuthorNames(author string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range splitAuthorSegments(author) {
		lower := strings.ToLower(part)
		if roleWord(lower) {
			continue
		}
		key := strings.ToLower(normalize(part))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, part)
	}
	return out
}

var roleWords = map[string]bool{
	"translator": true, "narrator": true, "illustrator": true,
	"foreword": true, "introduction": true, "afterword": true,
	"reader": true, "read by": true,
}

func roleWord(s string) bool {
	s = strings.TrimSpace(s)
	if roleWords[s] {
		return true
	}
	for _, rw := range []string{"read by ", "narrated by "} {
		if strings.HasPrefix(s, rw) {
			return true
		}
	}
	return false
}

// formatSeriesList renders the primary (and secondary, when present) series as
// "Name #<seq>" when a position is known, else as the plain series name.
func formatSeriesList(meta *models.BookMetadata) []string {
	var out []string
	if meta.Series != "" {
		out = append(out, formatSeriesEntry(meta.Series, meta.SeriesPosition))
	}
	if meta.SecondarySeries != "" && meta.SecondarySeries != meta.Series {
		out = append(out, formatSeriesEntry(meta.SecondarySeries, meta.SecondarySeriesPosition))
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func formatSeriesEntry(name string, pos float64) string {
	if pos > 0 {
		return fmt.Sprintf("%s #%s", name, strconv.FormatFloat(pos, 'f', -1, 64))
	}
	return name
}

// formatYear renders the published year as a 4-digit string ("2010").
func formatYear(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa(year)
}

// titleCase capitalizes the first letter of each word ("english" -> "English").
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		runes := []rune(strings.ToLower(w))
		if r := runes[0]; r >= 'a' && r <= 'z' {
			runes[0] = r - ('a' - 'A')
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

// yearFromDate extracts a 4-digit year from a "YYYY-MM-DD" release date
// (falls back to scanning for any 19xx/20xx year).
func yearFromDate(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil && y > 1000 && y < 3000 {
			return y
		}
	}
	yearRe := regexp.MustCompile(`(19|20)\d{2}`)
	if m := yearRe.FindString(date); m != "" {
		if y, err := strconv.Atoi(m); err == nil {
			return y
		}
	}
	return 0
}

// joinNames joins a list of {name} objects into a comma-separated string.
func joinNames(names []audnexAuthor) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if n.Name != "" {
			parts = append(parts, n.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// --- Persistent cache ---

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
