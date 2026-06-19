package main

import "strings"

func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var chunks []string
	start := 0

	for i, r := range text {
		switch r {
		case '.', '!', '?':
			end := i + 1
			s := strings.TrimSpace(text[start:end])
			if s != "" {
				chunks = append(chunks, s)
			}
			start = end
		}
	}

	if start < len(text) {
		s := strings.TrimSpace(text[start:])
		if s != "" {
			chunks = append(chunks, s)
		}
	}

	return chunks
}
