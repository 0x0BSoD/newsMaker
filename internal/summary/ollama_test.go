package summary

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/stretchr/testify/require"
)

func TestOllamaSummarizer_Summarize(t *testing.T) {
	t.Run("should make a summary", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/generate", r.URL.Path)

			var req api.GenerateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, "test-model", req.Model)
			require.Equal(t, "system prompt", req.System)
			require.Equal(t, "article text", req.Prompt)

			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"model":"test-model","response":"Hello, ","done":false}` + "\n"))
			_, _ = w.Write([]byte(`{"model":"test-model","response":"world!","done":true}` + "\n"))
		}))
		defer srv.Close()

		oc := NewOllamaSummarizer(strings.TrimPrefix(srv.URL, "http://"), "system prompt", "test-model", time.Minute)

		result, err := oc.Summarize(context.Background(), "article text")
		require.NoError(t, err)
		require.Equal(t, "Hello, world!", result)
	})
}

func TestCountTokens(t *testing.T) {
	for name, s := range map[string]interface {
		CountTokens(text string) (int, error)
	}{
		"ollama": &ollamaSummarizer{},
		"openai": &openAISummarizer{},
	} {
		t.Run(name, func(t *testing.T) {
			tokens, err := s.CountTokens("hello world, this is a token counting test")
			require.NoError(t, err)
			require.Greater(t, tokens, 0)
		})
	}
}
