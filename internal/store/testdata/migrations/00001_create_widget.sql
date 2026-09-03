-- +goose Up
CREATE TABLE widget (id integer PRIMARY KEY);

-- +goose Down
DROP TABLE widget;
