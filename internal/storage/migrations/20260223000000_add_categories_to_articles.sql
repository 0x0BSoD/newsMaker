-- +goose Up
-- +goose StatementBegin
ALTER TABLE articles ADD COLUMN categories TEXT[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE articles DROP COLUMN categories;
-- +goose StatementEnd
