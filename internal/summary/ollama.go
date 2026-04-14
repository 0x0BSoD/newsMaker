package summary

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tiktoken "github.com/hupe1980/go-tiktoken"
	"github.com/ollama/ollama/api"
)

type OllamaSummarizer struct {
	client  *api.Client
	prompt  string
	model   string
	timeout time.Duration
	mu      sync.Mutex
}

// CountTokens Simple estimation of token usage
func (o *OllamaSummarizer) CountTokens(text string) (int, error) {
	enc, err := tiktoken.NewEncodingForModel("ada")
	if err != nil {
		return 0, err
	}

	_, tokens, err := enc.Encode(text, nil, nil)
	if err != nil {
		return 0, err
	}

	return len(tokens), nil
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

func (o *OllamaSummarizer) Summarize(text string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	req := &api.GenerateRequest{
		Model:  o.model,
		System: o.prompt,
		Prompt: text,
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
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
