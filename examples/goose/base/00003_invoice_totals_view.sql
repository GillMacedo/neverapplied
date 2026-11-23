-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION account_total(acct BIGINT) RETURNS NUMERIC AS $$
BEGIN
    RETURN (SELECT coalesce(sum(amount), 0) FROM invoices WHERE account_id = acct);
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION account_total(BIGINT);
