package parser

import (
	"testing"
)

func TestParseName_StandardPatterns(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		parentName     string
		expectedTitle  string
		expectedAuthor string
		expectedSeries string
		minConfidence  int
		maxConfidence  int
	}{
		// Author - Title pattern (standard)
		{
			name:           "SimpleAuthorTitle",
			filename:       "Georgia Beers - Mine",
			parentName:     "",
			expectedAuthor: "Georgia Beers",
			expectedTitle:  "Mine",
			minConfidence:  65,
		},
		// Author - Title with ASIN
		{
			name:           "AuthorTitleASIN",
			filename:       "Radclyffe - The Practitioner [B0012JNFL0]",
			parentName:     "",
			expectedAuthor: "Radclyffe",
			expectedTitle:  "The Practitioner",
			minConfidence:  85,
		},
		// Title with ASIN only
		{
			name:          "TitleASIN",
			filename:      "The Hobbit [B00AHKQ1VC]",
			parentName:    "",
			expectedTitle: "The Hobbit",
			minConfidence: 65,
		},
		// Series Book N - Title pattern
		{
			name:            "SeriesBookNTitle",
			filename:        "Sherlock Holmes - Cases, Book 5 - The Final Problem",
			parentName:      "",
			expectedAuthor:  "Sherlock Holmes",
			expectedSeries:  "Cases",
			expectedTitle:   "The Final Problem",
			minConfidence:   90,
		},
		// Series Book N pattern (no title)
		{
			name:            "SeriesBookN",
			filename:        "J.K. Rowling - Harry Potter, Book 1",
			parentName:      "",
			expectedAuthor:  "J.K. Rowling",
			expectedSeries:  "Harry Potter",
			minConfidence:   80,
		},
		// [NN] Title pattern
		{
			name:          "BracketedNumberTitle",
			filename:      "[01] The Beginning",
			parentName:    "",
			expectedTitle: "The Beginning",
			minConfidence: 60,
		},
		// Series NN - Title pattern (e.g., "Pern 01 - Dragonflight")
		{
			name:            "WordNumberTitle",
			filename:        "Pern 01 - Dragonflight",
			parentName:      "",
			expectedSeries:  "Pern",
			expectedTitle:   "Dragonflight",
			minConfidence:   80,
		},
		// Series_Title -- Subtitle [ASIN]
		{
			name:           "UnderscoreSeriesTitleSubtitleASIN",
			filename:       "Crossfire_Bared to You -- Unmasked Encounters [B008Y4SQU2]",
			parentName:     "",
			expectedSeries: "Crossfire",
			expectedTitle:  "Bared to You",
			minConfidence:  75,
		},
		// Title - Author (reverse pattern)
		{
			name:           "ReversePattern",
			filename:       "Pride and Prejudice - Jane Austen",
			parentName:     "",
			expectedAuthor: "Jane Austen",
			expectedTitle:  "Pride and Prejudice",
			minConfidence:  70,
		},
		// Series (Author) pattern via heuristic — fallback to basic title
		// (This pattern typically gets matched when it's a directory name via scanner.go,
		// not a filename being parsed)
		{
			name:          "BasicTitleFallback",
			filename:      "Some Interesting Book Title",
			parentName:    "",
			expectedTitle: "Some Interesting Book Title",
			minConfidence: 30, // Fallback: entire name becomes title with low confidence
		},
		// Audio extension stripping
		{
			name:           "MP3ExtensionStrip",
			filename:       "Anne McCaffrey - Dragonflight.mp3",
			parentName:     "",
			expectedAuthor: "Anne McCaffrey",
			expectedTitle:  "Dragonflight",
			minConfidence:  65,
		},
		// M4B extension stripping
		{
			name:           "M4BExtensionStrip",
			filename:       "Terry Pratchett - Small Gods.m4b",
			parentName:     "",
			expectedAuthor: "Terry Pratchett",
			expectedTitle:  "Small Gods",
			minConfidence:  65,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseName(tt.filename, tt.parentName)

			if tt.expectedTitle != "" && parsed.Title != tt.expectedTitle {
				t.Errorf("Title: got %q, want %q", parsed.Title, tt.expectedTitle)
			}
			if tt.expectedAuthor != "" && parsed.Author != tt.expectedAuthor {
				t.Errorf("Author: got %q, want %q", parsed.Author, tt.expectedAuthor)
			}
			if tt.expectedSeries != "" && parsed.Series != tt.expectedSeries {
				t.Errorf("Series: got %q, want %q", parsed.Series, tt.expectedSeries)
			}

			if tt.minConfidence > 0 && parsed.Confidence < tt.minConfidence {
				t.Errorf("Confidence too low: got %d, want >= %d", parsed.Confidence, tt.minConfidence)
			}
			if tt.maxConfidence > 0 && parsed.Confidence > tt.maxConfidence {
				t.Errorf("Confidence too high: got %d, want <= %d", parsed.Confidence, tt.maxConfidence)
			}
		})
	}
}

func TestParseName_ConfidenceThreshold(t *testing.T) {
	// This test locks in the confidence threshold that the organizer.go
	// disambiguation logic depends on: parent-name-as-author guesses must
	// score <= 50, while filename-based author matches must score > 50.

	tests := []struct {
		name             string
		filename         string
		parentName       string
		expectConfidence func(conf int) bool
		description      string
	}{
		// Single-word parent name as author → low confidence (≤50)
		{
			name:       "SingleWordParentAsAuthor1",
			filename:   "Hunter 01 Hunter's Way",
			parentName: "Hunter",
			expectConfidence: func(conf int) bool {
				return conf <= 50
			},
			description: "Single-word parent guessed as author should be ≤50",
		},
		{
			name:       "SingleWordParentAsAuthor2",
			filename:   "Series 02 Some Title",
			parentName: "Series",
			expectConfidence: func(conf int) bool {
				return conf <= 50
			},
			description: "Single-word series guess should be ≤50",
		},
		// Actual filename-based author match → high confidence (>50)
		{
			name:       "FilenameAuthorMatch",
			filename:   "Georgia Beers - Mine",
			parentName: "",
			expectConfidence: func(conf int) bool {
				return conf > 50
			},
			description: "Filename-parsed author should be >50",
		},
		{
			name:       "FilenameAuthorMatchWithParent",
			filename:   "Georgia Beers - Mine",
			parentName: "SomeOtherDir",
			expectConfidence: func(conf int) bool {
				return conf > 50
			},
			description: "Filename-parsed author should remain >50 even with different parent",
		},
		// ASIN-matched title without author
		{
			name:       "ASINWithoutAuthor",
			filename:   "Some Book [B00AHKQ1VC]",
			parentName: "",
			expectConfidence: func(conf int) bool {
				return conf > 50
			},
			description: "ASIN confidence should be >50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseName(tt.filename, tt.parentName)
			if !tt.expectConfidence(parsed.Confidence) {
				t.Errorf("%s: got confidence %d, expectation failed", tt.description, parsed.Confidence)
			}
		})
	}
}

func TestParseName_DisambiguationFix(t *testing.T) {
	// The organizer.go bug fix uses these conditions:
	// if book.AuthorDir != "" && book.AuthorDir != parentName && parsed.Confidence <= 50 {
	//     if parser.IsAuthorish(book.AuthorDir) && !parser.IsTitleLike(book.AuthorDir) {
	//         parsed.Author = book.AuthorDir
	//         parsed.Confidence = 75
	//     }
	// }
	//
	// This tests that the parser's low-confidence author assignment happens,
	// so the organizer can catch and override it. The real fix happens in
	// organizer_test.go, but we verify the prerequisite here.

	tests := []struct {
		name              string
		filename          string
		parentName        string
		expectAuthor      string
		expectLowConfidence bool
	}{
		// Parent name guessed as author → should be extracted but low conf
		{
			name:               "ParentNameAsAuthorGuess",
			filename:           "Hunter 01 Hunter's Way",
			parentName:         "Hunter",
			expectAuthor:       "Hunter", // Guessed from parent
			expectLowConfidence: true,
		},
		// No author in filename, parent looks authorish
		{
			name:               "NoAuthorInFilename",
			filename:           "Book 02 Title",
			parentName:         "John Smith",
			expectAuthor:       "John Smith",
			expectLowConfidence: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseName(tt.filename, tt.parentName)

			if tt.expectAuthor != "" && parsed.Author != tt.expectAuthor {
				t.Errorf("Author: got %q, want %q", parsed.Author, tt.expectAuthor)
			}
			if tt.expectLowConfidence && parsed.Confidence > 50 {
				t.Errorf("Expected low confidence (<=50) for parent-name guess, got %d", parsed.Confidence)
			}
		})
	}
}

func TestIsAuthorish(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"SingleName", "John", true},
		{"TwoNames", "John Smith", true},
		{"ThreeNames", "John Michael Smith", true},
		{"FourNames", "John Michael Joseph Smith", true},
		{"TooManyNames", "John Michael Joseph Mary Smith", true}, // 5 words is at limit, so true
		{"Empty", "", false},
		{"WithNonAuthorKeyword", "John Smith gothic horror", false},
		{"SingleWordAudiobook", "audiobook", false},
		{"SingleWordSeries", "series", false},
		{"LongWord", "Xyzabcdefghijklmnop", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAuthorish(tt.input)
			if result != tt.expected {
				t.Errorf("IsAuthorish(%q): got %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsTitleLike(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"StartsWithThe", "The Great Gatsby", true},
		{"StartsWithA", "A Tale of Two Cities", true},
		{"StartsWithAn", "An Unexpected Journey", true},
		{"ContainsOf", "Lord of the Rings", true},
		{"ContainsAnd", "Pride and Prejudice", true},
		{"ManyWords", "The Complete Chronicles of Narnia Books 1-7", true},
		{"SingleName", "Hunter", false},
		{"TwoSimpleWords", "John Smith", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTitleLike(tt.input)
			if result != tt.expected {
				t.Errorf("IsTitleLike(%q): got %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseSeriesPosition(t *testing.T) {
	tests := []struct {
		name             string
		filename         string
		expectedPosition float64
	}{
		{"NumberedTitle1", "[01] The Beginning", 1},
		{"NumberedTitle2", "[05] Fifth Book", 5},
		{"UnbracketedNumber", "3 Third Book", 3},
		{"SeriesPattern", "Pern 07 - Seventh Book", 7},
		{"BookNumPattern", "Book 1.5 - Half Title", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseName(tt.filename, "")
			if parsed.SeriesPosition != tt.expectedPosition {
				t.Errorf("SeriesPosition: got %v, want %v", parsed.SeriesPosition, tt.expectedPosition)
			}
		})
	}
}

func TestParseASIN(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		expectedASIN  string
	}{
		{
			name:         "ASINAtEnd",
			filename:     "Some Book [B00AHKQ1VC]",
			expectedASIN: "B00AHKQ1VC",
		},
		{
			name:         "ASINWithAuthor",
			filename:     "Author - Title [B0012JNFL0]",
			expectedASIN: "B0012JNFL0",
		},
		{
			name:         "NoASIN",
			filename:     "Book Without ASIN",
			expectedASIN: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseName(tt.filename, "")
			if parsed.ASIN != tt.expectedASIN {
				t.Errorf("ASIN: got %q, want %q", parsed.ASIN, tt.expectedASIN)
			}
		})
	}
}
