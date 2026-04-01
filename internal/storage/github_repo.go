package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type GitHubRepo struct {
	FullName          string
	Topic             string
	Stars             int
	Language          string
	Description       string
	HTMLURL           string
	StarsAtLastDigest *int      // nil if never posted; populated on read
	FirstSeenAt       time.Time // populated on read
}

type GitHubRepoStorage interface {
	Upsert(ctx context.Context, repos []GitHubRepo) (newCount int, err error)
	MarkPosted(ctx context.Context, fullNames []string) error
	// LastPostedAt returns the most recent posted_at timestamp across all repos.
	// The second return value is false if no repo has been posted yet.
	LastPostedAt(ctx context.Context) (time.Time, bool, error)
	// GetNewAndTrending returns repos new since `since` and repos whose star count
	// grew by at least minGrowthPct (e.g. 0.30 for 30%) since the last digest.
	// New repos have first_seen_at > since; trending repos are existing ones with
	// significant star growth (requires stars_at_last_digest to be set).
	GetNewAndTrending(ctx context.Context, topic string, since time.Time, minGrowthPct float64) (newRepos []GitHubRepo, trending []GitHubRepo, err error)
}

type GitHubRepoPostgresStorage struct {
	db *sqlx.DB
}

func NewGitHubRepoStorage(db *sqlx.DB) *GitHubRepoPostgresStorage {
	return &GitHubRepoPostgresStorage{db: db}
}

// Upsert inserts new repos and updates last_seen_at, stars, language, and description
// for existing ones. Returns the count of repos that were inserted for the first time.
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
			`INSERT INTO github_repos (full_name, topic, stars, language, description, html_url)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (full_name) DO UPDATE
			     SET last_seen_at  = NOW(),
			         stars         = EXCLUDED.stars,
			         language      = EXCLUDED.language,
			         description   = EXCLUDED.description`,
			r.FullName, r.Topic, r.Stars, r.Language, r.Description, r.HTMLURL,
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

// MarkPosted sets posted_at = NOW() and snapshots stars_at_last_digest = stars
// for the given repo full names.
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
		`UPDATE github_repos
		 SET posted_at = $1, stars_at_last_digest = stars
		 WHERE full_name = ANY($2)`,
		time.Now().UTC(),
		pq.Array(fullNames),
	)
	return err
}

// LastPostedAt returns the most recent posted_at timestamp across all repos.
// Returns (zero, false, nil) if no repo has been posted yet.
func (s *GitHubRepoPostgresStorage) LastPostedAt(ctx context.Context) (time.Time, bool, error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	defer conn.Close()

	var ts pq.NullTime
	if err := conn.QueryRowxContext(ctx,
		`SELECT MAX(posted_at) FROM github_repos`,
	).Scan(&ts); err != nil {
		return time.Time{}, false, err
	}

	if !ts.Valid {
		return time.Time{}, false, nil
	}
	return ts.Time, true, nil
}

// GetNewAndTrending returns repos new since `since` and repos whose star count
// grew by at least minGrowthPct since the last digest.
// New repos: first_seen_at > since.
// Trending repos: existing repos (first_seen_at <= since) with stars_at_last_digest set
// and (stars - stars_at_last_digest) / stars_at_last_digest >= minGrowthPct.
func (s *GitHubRepoPostgresStorage) GetNewAndTrending(
	ctx context.Context, topic string, since time.Time, minGrowthPct float64,
) ([]GitHubRepo, []GitHubRepo, error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	type row struct {
		FullName          string    `db:"full_name"`
		Stars             int       `db:"stars"`
		Language          string    `db:"language"`
		StarsAtLastDigest *int      `db:"stars_at_last_digest"`
		Description       string    `db:"description"`
		HTMLURL           string    `db:"html_url"`
		FirstSeenAt       time.Time `db:"first_seen_at"`
	}

	var newRows []row
	if err := conn.SelectContext(ctx, &newRows,
		`SELECT full_name, stars, language, stars_at_last_digest, description, html_url, first_seen_at
		 FROM github_repos
		 WHERE topic = $1 AND first_seen_at > $2
		 ORDER BY stars DESC`,
		topic, since,
	); err != nil {
		return nil, nil, fmt.Errorf("query new repos: %w", err)
	}

	var trendingRows []row
	if err := conn.SelectContext(ctx, &trendingRows,
		`SELECT full_name, stars, language, stars_at_last_digest, description, html_url, first_seen_at
		 FROM github_repos
		 WHERE topic = $1
		   AND first_seen_at <= $2
		   AND stars_at_last_digest IS NOT NULL
		   AND stars_at_last_digest > 0
		   AND (stars - stars_at_last_digest)::float / stars_at_last_digest >= $3
		 ORDER BY (stars - stars_at_last_digest) DESC`,
		topic, since, minGrowthPct,
	); err != nil {
		return nil, nil, fmt.Errorf("query trending repos: %w", err)
	}

	toRepo := func(r row) GitHubRepo {
		return GitHubRepo{
			FullName:          r.FullName,
			Topic:             topic,
			Stars:             r.Stars,
			Language:          r.Language,
			StarsAtLastDigest: r.StarsAtLastDigest,
			Description:       r.Description,
			HTMLURL:           r.HTMLURL,
			FirstSeenAt:       r.FirstSeenAt,
		}
	}

	newRepos := make([]GitHubRepo, len(newRows))
	for i, r := range newRows {
		newRepos[i] = toRepo(r)
	}
	trending := make([]GitHubRepo, len(trendingRows))
	for i, r := range trendingRows {
		trending[i] = toRepo(r)
	}
	return newRepos, trending, nil
}
