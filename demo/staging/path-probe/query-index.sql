-- First demo query: one physical index, two candidate identities.  The base
-- restriction makes an unparameterized path over pp_orders_customer_idx worth
-- building; the join makes a parameterized path over the same index possible.
--
-- A bare query, not EXPLAIN: probe.sql passes the text to pathprobe() and
-- pathprobe_json(), which plan it without executing it.
SELECT o.id, o.customer_id, o.amount
FROM pp_customers AS c
JOIN pp_orders AS o
  ON o.customer_id = c.id
WHERE c.id BETWEEN 100 AND 109
  AND o.customer_id > 0
