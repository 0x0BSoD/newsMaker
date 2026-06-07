-- +goose Up
-- +goose StatementBegin
ALTER TABLE github_repos ADD COLUMN stars_at_last_digest INT;
ALTER TABLE github_repos ADD COLUMN language TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE github_repos DROP COLUMN stars_at_last_digest;
ALTER TABLE github_repos DROP COLUMN language;
-- +goose StatementEnd
