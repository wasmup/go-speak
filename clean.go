package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const url = "http://localhost:8011/v1/chat/completions"

const systemPrompt = "You are a text-processing assistant. Your task is to prepare input text for a Text-to-Speech (TTS) engine. " +
	"1. Reconstruct words that are hyphenated across line breaks (e.g., 'config-\nuration' becomes 'configuration'). " +
	"2. Remove meaningless or noisy strings, random hashes, hexadecimal codes, or bracketed gibberish (e.g., '0x9F4D2A7B', '[abc123xyz_hash]'). " +
	"3. Preserve the natural flow and punctuation of the sentence. " +
	"4. Output ONLY the finalized clean text. Do not write explanations, introductions, or wrappers."

// OpenAI-compatible Chat Completion structures
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

func Clean(input string) (string, error) {
	reqBody := ChatRequest{
		Model: "ornith-1.0-9b",
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: input},
		},
		Temperature: 0.1,
		MaxTokens:   512,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Println("LLM clean latency:", time.Since(start))

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, string(b))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}

	out := strings.TrimSpace(chatResp.Choices[0].Message.Content)

	// guard against models returning explanations
	if strings.HasPrefix(out, "Here") || strings.Contains(out, "cleaned text") {
		return input, fmt.Errorf("model returned commentary")
	}

	return out, nil
}

var (
	reHyphenBreak = regexp.MustCompile(`[A-Za-z]-\s*\n\s*[A-Za-z]`)
	reLongHex     = regexp.MustCompile(`\b[0-9A-Fa-f]{8,}\b`)
	reBracketHash = regexp.MustCompile(`\[[^\]]{6,}\]`)
	reWeirdToken  = regexp.MustCompile(`\b[A-Za-z0-9]{15,}\b`)
)

// needsLLMClean decides whether the text likely contains
// structural damage or high-noise artifacts that require LLM repair.
func needsLLMClean(text string) bool {
	if len(text) < 20 {
		return false
	}

	// 1. Hyphenated line breaks (classic PDF issue)
	if reHyphenBreak.MatchString(text) {
		return true
	}

	// 2. Long hex blobs
	if reLongHex.MatchString(text) {
		return true
	}

	// 3. Suspicious bracket garbage (not short citations like [1])
	if reBracketHash.MatchString(text) {
		return true
	}

	// 4. Extremely long alphanumeric garbage tokens
	if reWeirdToken.MatchString(text) {
		return true
	}

	// 5. High symbol density (noise detection)
	if highSymbolRatio(text) {
		return true
	}

	return false
}

func highSymbolRatio(s string) bool {
	var letters, symbols int

	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r):
			letters++
		default:
			symbols++
		}
	}

	total := letters + symbols
	if total == 0 {
		return false
	}

	ratio := float64(symbols) / float64(total)

	// threshold tuned for OCR junk
	return ratio > 0.25
}
