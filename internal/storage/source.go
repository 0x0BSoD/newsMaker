package storage

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/samber/lo"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

type SourcePostgresStorage struct {
	db *sqlx.DB
}

func NewSourceStorage(db *sqlx.DB) *SourcePostgresStorage {
	return &SourcePostgresStorage{db: db}
}

func (s *SourcePostgresStorage) Sources(ctx context.Context) ([]model.Source, error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var sources []dbSource
	if err := conn.SelectContext(ctx, &sources, `SELECT * FROM sources`); err != nil {
		return nil, err
	}

	return lo.Map(sources, func(source dbSource, _ int) model.Source { return source.toModel() }), nil
}

func (s *SourcePostgresStorage) SourceByID(ctx context.Context, id int64) (*model.Source, error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var source dbSource
	if err := conn.GetContext(ctx, &source, `SELECT * FROM sources WHERE id = $1`, id); err != nil {
		return nil, err
	}

	m := source.toModel()
	return &m, nil
}

func (s *SourcePostgresStorage) Add(ctx context.Context, source model.Source) (int64, error) {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	var id int64

	var scraperCfg *dbScraperConfig
	if source.ScraperConfig != nil {
		scraperCfg = &dbScraperConfig{*source.ScraperConfig}
	}

	row := conn.QueryRowxContext(
		ctx,
		`INSERT INTO sources (name, feed_url, priority, insecure, source_type, scraper_config)
					VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;`,
		source.Name, source.FeedURL, source.Priority, source.Insecure, source.SourceType, scraperCfg,
	)

	if err := row.Err(); err != nil {
		return 0, err
	}

	if err := row.Scan(&id); err != nil {
		return 0, err
	}

	return id, nil
}

func (s *SourcePostgresStorage) SetPriority(ctx context.Context, id int64, priority int) error {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx, `UPDATE sources SET priority = $1 WHERE id = $2`, priority, id)

	return err
}

func (s *SourcePostgresStorage) Delete(ctx context.Context, id int64) error {
	conn, err := s.db.Connx(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `DELETE FROM sources WHERE id = $1`, id); err != nil {
		return err
	}

	return nil
}

type dbScraperConfig struct {
	model.ScraperConfig
}

func (c *dbScraperConfig) Scan(src any) error {
	if src == nil {
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("dbScraperConfig: expected []byte, got %T", src)
	}
	return json.Unmarshal(b, &c.ScraperConfig)
}

func (c dbScraperConfig) Value() (driver.Value, error) {
	b, err := json.Marshal(c.ScraperConfig)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type dbSource struct {
	ID            int64           `db:"id"`
	Name          string          `db:"name"`
	FeedURL       string          `db:"feed_url"`
	Priority      int             `db:"priority"`
	Insecure      bool            `db:"insecure"`
	SourceType    string          `db:"source_type"`
	ScraperConfig *dbScraperConfig `db:"scraper_config"`
	CreatedAt     time.Time       `db:"created_at"`
}

func (s dbSource) toModel() model.Source {
	m := model.Source{
		ID:         s.ID,
		Name:       s.Name,
		FeedURL:    s.FeedURL,
		Priority:   s.Priority,
		Insecure:   s.Insecure,
		SourceType: s.SourceType,
		CreatedAt:  s.CreatedAt,
	}
	if s.ScraperConfig != nil {
		cfg := s.ScraperConfig.ScraperConfig
		m.ScraperConfig = &cfg
	}
	return m
}
