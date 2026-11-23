CREATE TABLE orders (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users (id),
    total_cents BIGINT NOT NULL,
    coupon_code TEXT
);
