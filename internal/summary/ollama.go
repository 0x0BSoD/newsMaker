package summary

import (
	"context"
	"fmt"
	"log"
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
	enabled bool
	mu      sync.Mutex
}

func NewOllamaSummarizer(baseURL, prompt, model string) *OllamaSummarizer {
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
		enabled: true,
	}
}

func (o *OllamaSummarizer) Summarize(text string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	log.Printf("[INFO] Running OLLAMA Summarizer...")
	req := &api.GenerateRequest{
		Model:  o.model,
		Prompt: fmt.Sprintf("%s\n%s", o.prompt, text),
		Stream: nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var responseFlow []string
	err := o.client.Generate(ctx, req, func(resp api.GenerateResponse) error {
		log.Printf("[INFO] OLLAMA Summarizer, working...")
		responseFlow = append(responseFlow, resp.Response)
		return nil
	})
	if err != nil {
		return "", err
	}

	return strings.Join(responseFlow, ""), nil
}
