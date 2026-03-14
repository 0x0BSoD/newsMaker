package storage

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type GitHubRepo struct {
	FullName    string
	Topic       string
	Stars       int
	Description string
	HTMLURL     string
}

type GitHubRepoStorage interface {
	Upsert(ctx context.Context, repos []GitHubRepo) (newCount int, err error)
	MarkPosted(ctx context.Context, fullNames []string) error
}

type GitHubRepoPostgresStorage struct {
	db *sqlx.DB
}

func NewGitHubRepoStorage(db *sqlx.DB) *GitHubRepoPostgresStorage {
	return &GitHubRepoPostgresStorage{db: db}
}

// Upsert inserts new repos and updates last_seen_at for existing ones.
// Returns the count of repos that were inserted for the first time.
func (s *GitHubRepoPostgresStorage) Upsert(ctx context.Context, repos []GitHubRepo) (int, error) {
	if len(repos) == 0 {
		return 0, nil
	}

	conn, err := s.db.Connx(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	fullNames := make([]string, len(repos))
	for i, r := range repos {
		fullNames[i] = r.FullName
	}

	var existing []string
	if err := conn.SelectContext(ctx, &existing,
		`SELECT full_name FROM github_repos WHERE full_name = ANY($1)`,
		pq.Array(fullNames),
	); err != nil {
		return 0, err
	}

	existingSet := make(map[string]bool, len(existing))
	for _, fn := range existing {
		existingSet[fn] = true
	}

	for _, r := range repos {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO github_repos (full_name, topic, stars, description, html_url)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (full_name) DO UPDATE
			     SET last_seen_at  = NOW(),
			         stars         = EXCLUDED.stars,
			         description   = EXCLUDED.description`,
			r.FullName, r.Topic, r.Stars, r.Description, r.HTMLURL,
		); err != nil {
			return 0, err
		}
	}

	newCount := 0
	for _, r := range repos {
		if !existingSet[r.FullName] {
			newCount++
		}
	}
	return newCount, nil
}

// MarkPosted sets posted_at = NOW() for the given repo full names.
func (s *GitHubRepoPostgresStorage) MarkPosted(ctx context.Context, fullNames []string) error {
	if len(fullNames) == 0 {
		return nil
	}

	conn, err := s.db.Connx(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx,
		`UPDATE github_repos SET posted_at = $1 WHERE full_name = ANY($2)`,
		time.Now().UTC(),
		pq.Array(fullNames),
	)
	return err
}
