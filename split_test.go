package main

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestSentenceSplitterSplitPreservesEndingPunctuation(t *testing.T) {
	splitter := NewSentenceSplitter()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "sentence punctuation",
			text: "First sentence. Is this working? It is!",
			want: []string{"First sentence.", "Is this working?", "It is!"},
		},
		{
			name: "punctuation followed by closing quote",
			text: "He said \"Hello.\" Then he left.",
			want: []string{"He said \"Hello.\"", "Then he left."},
		},
		{
			name: "abbreviations",
			text: "Dr. Smith arrived. Mr. Jones left.",
			want: []string{"Dr. Smith arrived.", "Mr. Jones left."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitter.Split(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Split(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}

func TestCustomDelimitersAndLiteralTokens(t *testing.T) {
	s := NewSentenceSplitter()
	for _, tc := range []struct {
		text, delimiters string
		want             []string
	}{
		{"One; Two.", ";", []string{"One;", "Two."}},
		{"One- Two.", "-", []string{"One-", "Two."}},
		{"__ABBR1__ is literal. Dr. Smith left.", "", []string{"__ABBR1__ is literal.", "Dr. Smith left."}},
		{"\uE000 is literal. Next!", "", []string{"\uE000 is literal.", "Next!"}},
	} {
		if got := s.SplitWithDelimiters(tc.text, tc.delimiters); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %#v, want %#v", got, tc.want)
		}
	}
}

func FuzzSentenceDelimiters(f *testing.F) {
	for _, chars := range []string{"-", "]", "\\", "[a-z]", ".!?"} {
		f.Add(chars)
	}
	f.Fuzz(func(t *testing.T, chars string) {
		// The HTTP layer accepts only bounded, valid UTF-8 delimiter strings.
		if len(chars) > 128 || !utf8.ValidString(chars) {
			return
		}
		NewSentenceSplitter().SplitWithDelimiters("A sentence. Another sentence!", chars)
	})
}
