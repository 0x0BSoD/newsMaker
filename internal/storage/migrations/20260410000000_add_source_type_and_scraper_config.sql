-- +goose Up
-- +goose StatementBegin
ALTER TABLE sources ADD COLUMN source_type TEXT NOT NULL DEFAULT 'rss';
ALTER TABLE sources ADD COLUMN scraper_config JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sources DROP COLUMN scraper_config;
ALTER TABLE sources DROP COLUMN source_type;
-- +goose StatementEnd
