-- +goose Up
ALTER TABLE widget ADD COLUMN name text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE widget DROP COLUMN name;
