-- +goose Up
-- +goose StatementBegin
ALTER TABLE articles ALTER COLUMN title TYPE TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE articles
    ALTER COLUMN title TYPE VARCHAR(255)
    USING LEFT(title, 255);
-- +goose StatementEnd
