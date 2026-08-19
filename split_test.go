package main

import (
	"reflect"
	"testing"
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
