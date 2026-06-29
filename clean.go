package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const url = "http://localhost:8011/v1/chat/completions"

const systemPrompt = "You are a text-processing assistant. Your task is to prepare input text for a Text-to-Speech (TTS) engine. " +
	"1. Reconstruct words that are hyphenated across line breaks (e.g., 'config-\nuration' becomes 'configuration'). " +
	"2. Remove meaningless or noisy strings, random hashes, hexadecimal codes, or bracketed gibberish (e.g., '0x9F4D2A7B', '[abc123xyz_hash]'). " +
	"3. Preserve the natural flow and punctuation of the sentence. " +
	"4. Output ONLY the finalized clean text. Do not write explanations, introductions, or wrappers."

func Clean(input string) (string, error) {
	r := ChatRequest{
		Model: "ornith-1.0-9b", // llama-server ignores this or matches it to the loaded model
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: input},
		},
		// We set a slightly lower temp than your server default to keep formatting strict
		Temperature: 0.1,
	}

	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(b))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	fmt.Println(`clean text by LLM ...`)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	d := time.Since(start)
	fmt.Println(`clean text by LLM done`, d)

	if resp.StatusCode != http.StatusOK {
		bb, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(bb))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("received empty choices array from model")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// OpenAI-compatible Chat Completion structures
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}
