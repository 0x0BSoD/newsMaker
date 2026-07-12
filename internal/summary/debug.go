package summary

import (
	"log/slog"
	"os"
	"path/filepath"
)

// WriteDebugFile saves LLM input/output text to a file in dir for inspection.
// Errors are logged but never fail the calling flow.
func WriteDebugFile(dir, filename, content string) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Warn("failed to write summary debug file", "path", path, "err", err)
	}
}
