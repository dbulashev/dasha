// Package stream names the log streams the API serves; a source may declare no
// others. It holds them alone so the packages that classify records need no
// part of the configuration.
package stream

const (
	PostgreSQL = "postgresql"
	Pooler     = "pooler"
)
