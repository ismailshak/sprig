-- +goose Up
CREATE TABLE gadget (id integer PRIMARY KEY;

-- +goose Down
DROP TABLE gadget;
