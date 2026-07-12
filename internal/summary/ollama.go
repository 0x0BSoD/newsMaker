package summary

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

type ollamaSummarizer struct {
	client  *api.Client
	prompt  string
	model   string
	timeout time.Duration
}

// CountTokens Simple estimation of token usage
func (o *ollamaSummarizer) CountTokens(text string) (int, error) {
	return estimateTokens(text)
}

// NewOllamaSummarizer creates a summarizer backed by a local Ollama server.
func NewOllamaSummarizer(baseURL, prompt, model string, timeout time.Duration) Summarizer {
	// The context deadline in Summarize covers the request, but the client
	// timeout bounds connection establishment even without a deadline.
	httpClient := &http.Client{Timeout: timeout}

	c := api.NewClient(&url.URL{
		Scheme: "http",
		Host:   baseURL,
		Path:   "/",
	}, httpClient)

	return &ollamaSummarizer{
		client:  c,
		prompt:  prompt,
		model:   model,
		timeout: timeout,
	}
}

func (o *ollamaSummarizer) Summarize(ctx context.Context, text string) (string, error) {
	req := &api.GenerateRequest{
		Model:  o.model,
		System: o.prompt,
		Prompt: text,
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	var responseFlow []string
	err := o.client.Generate(ctx, req, func(resp api.GenerateResponse) error {
		responseFlow = append(responseFlow, resp.Response)
		return nil
	})
	if err != nil {
		return "", err
	}

	return strings.Join(responseFlow, ""), nil
}
