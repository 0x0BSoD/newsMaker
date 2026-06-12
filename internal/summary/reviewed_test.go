package summary

import (
	"context"
	"errors"
	"testing"
)

type fakeSummarizer struct {
	out   string
	err   error
	calls []string
}

func (f *fakeSummarizer) Summarize(_ context.Context, text string) (string, error) {
	f.calls = append(f.calls, text)
	return f.out, f.err
}

func (f *fakeSummarizer) CountTokens(text string) (int, error) {
	return len(text), nil
}

func TestReviewed_Summarize(t *testing.T) {
	tests := []struct {
		name     string
		drafter  *fakeSummarizer
		reviewer *fakeSummarizer
		want     string
		wantErr  bool
	}{
		{
			name:     "reviewer output wins",
			drafter:  &fakeSummarizer{out: "draft"},
			reviewer: &fakeSummarizer{out: "polished"},
			want:     "polished",
		},
		{
			name:     "reviewer error falls back to draft",
			drafter:  &fakeSummarizer{out: "draft"},
			reviewer: &fakeSummarizer{err: errors.New("boom")},
			want:     "draft",
		},
		{
			name:     "reviewer empty output falls back to draft",
			drafter:  &fakeSummarizer{out: "draft"},
			reviewer: &fakeSummarizer{out: "  \n "},
			want:     "draft",
		},
		{
			name:     "drafter error propagates",
			drafter:  &fakeSummarizer{err: errors.New("boom")},
			reviewer: &fakeSummarizer{out: "polished"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewReviewed(tt.drafter, tt.reviewer).Summarize(context.Background(), "input")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReviewed_ReviewerGetsDraft(t *testing.T) {
	drafter := &fakeSummarizer{out: "draft"}
	reviewer := &fakeSummarizer{out: "polished"}

	if _, err := NewReviewed(drafter, reviewer).Summarize(context.Background(), "input"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reviewer.calls) != 1 || reviewer.calls[0] != "draft" {
		t.Errorf("reviewer called with %v, want [draft]", reviewer.calls)
	}
}
