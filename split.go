package main

import (
	"fmt"
	"regexp"
	"strings"
)

func splitSentences(text, splitChars string) []string {
	f := func(r rune) bool {
		return strings.ContainsRune(splitChars, r)
	}
	parts := strings.FieldsFunc(text, f)

	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

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

	for i, a := range abbrs {
		token := fmt.Sprintf("__ABBR%d__", i)
		abbrMap[a] = token
	}

	return &SentenceSplitter{
		abbr: abbrMap,
		re:   regexp.MustCompile(`([.!?])\s+`),
	}
}

func (s *SentenceSplitter) protectAbbreviations(text string) string {
	for abbr, token := range s.abbr {
		text = strings.ReplaceAll(text, abbr, token)
	}
	return text
}

func (s *SentenceSplitter) restoreAbbreviations(text string) string {
	for abbr, token := range s.abbr {
		text = strings.ReplaceAll(text, token, abbr)
	}
	return text
}

func (s *SentenceSplitter) Split(text string) []string {
	text = s.protectAbbreviations(text)

	parts := s.re.Split(text, -1)

	sentences := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		p = s.restoreAbbreviations(p)
		sentences = append(sentences, p)
	}

	return sentences
}
