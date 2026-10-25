-- Second demo query, the motivating case: a wider outer relation.  Here the
-- index does not appear in the final plan at all.
--
-- With the injection point series the trace showed both candidates over
-- pp_orders_customer_idx accepted, including the parameterized one, and the
-- join level ending with a Hash Join over a Seq Scan.  pathprobe should show
-- the same lifecycle, plus the join methods skipped by precheck by name.
--
-- A bare query, not EXPLAIN: probe.sql passes the text to pathprobe() and
-- pathprobe_json(), which plan it without executing it.
SELECT o.id, o.customer_id, o.amount
FROM pp_customers AS c
JOIN pp_orders AS o
  ON o.customer_id = c.id
WHERE c.id <= 5000
  AND o.customer_id > 0
