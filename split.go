package main

import (
	"regexp"
	"strings"
)

type SentenceSplitter struct {
	abbr map[string]string
	re   *regexp.Regexp
}

func NewSentenceSplitter() *SentenceSplitter {
	// abbreviations that should not trigger sentence breaks
	abbrs := []string{
		"Mr.", "Mrs.", "Ms.", "Dr.", "Prof.",
		"Sr.", "Jr.", "St.",
		"vs.", "etc.", "Fig.", "fig.",
		"e.g.", "i.e.",
		"U.S.", "U.K.", "U.N.",
	}

	abbrMap := make(map[string]string)
	for _, a := range abbrs {
		abbrMap[a] = a
	}

	return &SentenceSplitter{
		abbr: abbrMap,
		// Keep the sentence-ending punctuation (and any immediately following
		// closing quotes/brackets) with the sentence passed to TTS.
		re: regexp.MustCompile(`[.!?]+[”"')\]]*\s+`),
	}
}

func (s *SentenceSplitter) Split(text string) []string { return s.SplitWithDelimiters(text, "") }

func (s *SentenceSplitter) SplitWithDelimiters(text, delimiters string) []string {
	marker := "\uE000"
	for strings.Contains(text, marker) {
		marker += "\uE001"
	}
	for abbr := range s.abbr {
		text = strings.ReplaceAll(text, abbr, strings.ReplaceAll(abbr, ".", marker))
	}
	re := s.re
	if delimiters != "" {
		re = regexp.MustCompile("[" + strings.ReplaceAll(regexp.QuoteMeta(delimiters), "-", `\-`) + `]+[”"')\]]*\s+`)
	}

	sentences := make([]string, 0)
	start := 0
	for _, match := range re.FindAllStringIndex(text, -1) {
		// The match includes the delimiter and following whitespace, so slicing
		// through it before trimming retains the delimiter.
		p := strings.TrimSpace(text[start:match[1]])
		if p == "" {
			start = match[1]
			continue
		}

		p = strings.ReplaceAll(p, marker, ".")
		sentences = append(sentences, p)
		start = match[1]
	}

	if p := strings.TrimSpace(text[start:]); p != "" {
		sentences = append(sentences, strings.ReplaceAll(p, marker, "."))
	}

	return sentences
}
