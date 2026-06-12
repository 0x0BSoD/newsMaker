package summary

import (
	"context"
	"log/slog"
	"strings"
)

// Summarizer is the common contract of the LLM backends in this package.
type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
	CountTokens(text string) (int, error)
}

// Reviewed runs a two-stage pipeline: the drafter writes the post, then the
// reviewer (a second LLM call with a copy-editor system prompt) checks the
// Telegram HTML syntax and overall quality and returns the corrected text.
// The review stage is best-effort polish, not a gate: if it fails or returns
// nothing, the draft is posted as-is.
type Reviewed struct {
	drafter  Summarizer
	reviewer Summarizer
}

func NewReviewed(drafter, reviewer Summarizer) *Reviewed {
	return &Reviewed{drafter: drafter, reviewer: reviewer}
}

func (r *Reviewed) Summarize(ctx context.Context, text string) (string, error) {
	draft, err := r.drafter.Summarize(ctx, text)
	if err != nil {
		return "", err
	}

	reviewed, err := r.reviewer.Summarize(ctx, draft)
	if err != nil {
		slog.Warn("post review failed, keeping unreviewed draft", "err", err)
		return draft, nil
	}
	if strings.TrimSpace(reviewed) == "" {
		slog.Warn("post review returned empty text, keeping unreviewed draft")
		return draft, nil
	}

	return reviewed, nil
}

func (r *Reviewed) CountTokens(text string) (int, error) {
	return r.drafter.CountTokens(text)
}
