package postgres

// listDatabasesQuery lists the databases this role may connect to: template
// databases and those with connections disabled are left out. ORDER BY keeps
// the published list stable between cycles.
const listDatabasesQuery = `SELECT datname FROM pg_database
 WHERE NOT datistemplate
   AND datallowconn
   AND has_database_privilege(datname, 'CONNECT')
 ORDER BY datname`
