-- +goose Up
-- +goose StatementBegin
ALTER TABLE sources ADD COLUMN insecure BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sources DROP COLUMN insecure;
-- +goose StatementEnd
