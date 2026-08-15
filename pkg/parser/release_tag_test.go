package parser

import "testing"

func TestReleaseTagStrip(t *testing.T) {
	cases := []struct{ in, wantAuthor, wantTitle string }{
		{"[M4B] Andy Weir - Project Hail Mary", "Andy Weir", "Project Hail Mary"},
		{"[AudioBook] Frank Herbert - Dune Messiah", "Frank Herbert", "Dune Messiah"},
		{"[8] The Way of Kings", "", "The Way of Kings"}, // series-position bracket kept
		{"[M4B] Solo Leveling - Chugong", "Solo Leveling", "Chugong"},
	}
	for _, c := range cases {
		info := ParseName(c.in, "")
		if info.Author != c.wantAuthor {
			t.Errorf("%q: author = %q, want %q", c.in, info.Author, c.wantAuthor)
		}
		if info.Title != c.wantTitle {
			t.Errorf("%q: title = %q, want %q", c.in, info.Title, c.wantTitle)
		}
	}
}
