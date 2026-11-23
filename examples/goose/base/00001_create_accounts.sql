-- +goose Up
CREATE TABLE accounts (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE accounts;
