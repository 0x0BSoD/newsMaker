package summary

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

type OllamaSummarizer struct {
	client  *api.Client
	prompt  string
	model   string
	timeout time.Duration
}

// CountTokens Simple estimation of token usage
func (o *OllamaSummarizer) CountTokens(text string) (int, error) {
	return estimateTokens(text)
}

func NewOllamaSummarizer(baseURL, prompt, model string, timeout time.Duration) *OllamaSummarizer {
	httpClient := &http.Client{}

	c := api.NewClient(&url.URL{
		Scheme: "http",
		Host:   baseURL,
		Path:   "/",
	}, httpClient)

	return &OllamaSummarizer{
		client:  c,
		prompt:  prompt,
		model:   model,
		timeout: timeout,
	}
}

func (o *OllamaSummarizer) Summarize(ctx context.Context, text string) (string, error) {
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
