DROP TABLE IF EXISTS pp_customers;
DROP TABLE IF EXISTS pp_orders;

-- No autovacuum: it fills the visibility map minutes after start, Index Only Scan
-- gets cheaper and the join demo takes another search path.
CREATE TABLE pp_customers
(
    id      bigint PRIMARY KEY,
    segment integer NOT NULL
) WITH (autovacuum_enabled = off);

CREATE TABLE pp_orders
(
    id          bigint PRIMARY KEY,
    customer_id bigint NOT NULL,
    amount      integer NOT NULL,
    payload     text NOT NULL
) WITH (autovacuum_enabled = off);

INSERT INTO pp_customers
SELECT g, g % 100
FROM generate_series(1, 10000) AS g;

INSERT INTO pp_orders
SELECT g,
       1 + (g % 10000),
       g % 5000,
       repeat('x', 80)
FROM generate_series(1, 1000000) AS g;

CREATE INDEX pp_orders_customer_idx ON pp_orders(customer_id);

ANALYZE pp_customers;
ANALYZE pp_orders;
