-- +goose Up
-- +goose StatementBegin
CREATE TABLE github_repos
(
    id            BIGSERIAL PRIMARY KEY,
    full_name     TEXT      NOT NULL UNIQUE,
    topic         TEXT      NOT NULL,
    stars         INT       NOT NULL DEFAULT 0,
    description   TEXT      NOT NULL DEFAULT '',
    html_url      TEXT      NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at     TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS github_repos;
-- +goose StatementEnd
