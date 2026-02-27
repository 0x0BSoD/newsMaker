package summary

import (
	"context"
	"fmt"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type OpenAISummarizer struct {
	client  *openai.Client
	prompt  string
	model   string
	timeout time.Duration
}

// NewOpenAISummarizer creates a summarizer backed by any OpenAI-compatible API.
// Set baseURL to a non-empty string to point at a local server (LM Studio,
// llama.cpp, Ollama's /v1 endpoint, etc.); leave empty for api.openai.com.
func NewOpenAISummarizer(baseURL, apiKey, prompt, model string, timeout time.Duration) *OpenAISummarizer {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &OpenAISummarizer{
		client:  openai.NewClientWithConfig(cfg),
		prompt:  prompt,
		model:   model,
		timeout: timeout,
	}
}

func (o *OpenAISummarizer) Summarize(text string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: o.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: o.prompt},
			{Role: openai.ChatMessageRoleUser, Content: text},
		},
	})
	if err != nil {
		return "", fmt.Errorf("chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from model %q", o.model)
	}

	return resp.Choices[0].Message.Content, nil
}
