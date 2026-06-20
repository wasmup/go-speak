package main

import "strings"

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
