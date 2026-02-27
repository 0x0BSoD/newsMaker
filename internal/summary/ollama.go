package summary

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ollama/ollama/api"
)

type OllamaSummarizer struct {
	client  *api.Client
	prompt  string
	model   string
	timeout time.Duration
	mu      sync.Mutex
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
