-- +goose Up
CREATE TABLE invoices (
    id         BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts (id),
    amount     NUMERIC(12, 2) NOT NULL
);

-- +goose Down
DROP TABLE invoices;
