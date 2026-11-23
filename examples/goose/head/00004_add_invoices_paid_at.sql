-- +goose Up
ALTER TABLE invoices ADD COLUMN paid_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE invoices DROP COLUMN paid_at;
