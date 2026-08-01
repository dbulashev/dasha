-- has_schema_privilege() throws on a missing role and would kill the whole query;
-- aclexplode over nspacl covers every schema at once. grantee 0 is PUBLIC,
-- and acldefault() expands a NULL acl into the owner's implicit grants.
SELECT
    n.nspname                              AS schema_name,
    pg_catalog.pg_get_userbyid(n.nspowner) AS owner
FROM pg_catalog.pg_namespace n
WHERE n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  AND EXISTS (
      SELECT 1
      FROM aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) a
      WHERE a.grantee = 0 AND a.privilege_type = 'CREATE'
  )
ORDER BY n.nspname
LIMIT $1
