package main

import (
	"bufio"
	"bytes"
	"embed"
	"regexp"
	"strings"
)

// wget -c https://raw.githubusercontent.com/dwyl/english-words/master/words_alpha.txt

//go:embed dict/words_alpha.txt
var dictFS embed.FS

type Cleaner struct {
	dict             map[string]struct{}
	noiseRegex       *regexp.Regexp
	hyphenLineRegex  *regexp.Regexp
	hyphenSpaceRegex *regexp.Regexp
	spaceRegex       *regexp.Regexp
}

func NewCleaner() (*Cleaner, error) {
	data, err := dictFS.ReadFile("dict/words_alpha.txt")
	if err != nil {
		return nil, err
	}

	dict := make(map[string]struct{}, 400000)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		w := strings.TrimSpace(scanner.Text())
		if w != "" {
			dict[w] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &Cleaner{
		dict: dict,

		noiseRegex: regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\[[^\]]+\]|\b[a-zA-Z]*\d{2,}[a-zA-Z\d]*\b`),

		// word-\nword
		hyphenLineRegex: regexp.MustCompile(`(?m)(\b[a-zA-Z]+)-\s*\n\s*([a-zA-Z]+\b)`),

		// word- word   (single or multiple spaces, but NOT newline)
		hyphenSpaceRegex: regexp.MustCompile(`(\b[a-zA-Z]+)-\s+([a-zA-Z]+\b)`),

		spaceRegex: regexp.MustCompile(`[ \t]+`),
	}, nil
}

func (c *Cleaner) isWord(w string) bool {
	_, ok := c.dict[strings.ToLower(w)]
	return ok
}

func (c *Cleaner) Clean(text string) string {
	// 1. Remove noise
	text = c.noiseRegex.ReplaceAllString(text, "")

	// 2. Fix newline hyphenation (sec-\nond)
	text = c.hyphenLineRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := c.hyphenLineRegex.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		return c.mergeOrHyphen(sub[1], sub[2])
	})

	// 3. Fix space hyphenation (sec- ond)
	text = c.hyphenSpaceRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := c.hyphenSpaceRegex.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		return c.mergeOrHyphen(sub[1], sub[2])
	})

	// 4. Normalize whitespace
	text = c.spaceRegex.ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(text, "\n")

	return strings.TrimSpace(text)
}

func (c *Cleaner) mergeOrHyphen(part1, part2 string) string {
	merged := part1 + part2
	hyphenated := part1 + "-" + part2

	// Prefer merged if valid English word
	if c.isWord(merged) {
		return merged
	}

	// If merged invalid but hyphenated looks valid, keep hyphen
	if c.isWord(part1) && c.isWord(part2) {
		return hyphenated
	}

	// Default: merged (safer for TTS)
	return merged
}
