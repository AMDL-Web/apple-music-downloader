package db

import (
	"context"
	"database/sql"
)

// migrateTo8 detaches the runs that name a record which does not exist: it
// stores no record (NULL resource_kind and resource_id, as a station run
// has) on each of them. They are the finished album and pl. playlist runs
// the 1.x import named a record for although it made none, and the runs a
// record delete left behind before it deleted a record's runs. A run a
// pending add will still attach to keeps its record: an add of the same kind
// and catalog id (the import's stopped jobs), or a song add whose album hint
// is the run's album; both resolve into a record with that id. Rows are
// kept; only those two columns change. Frozen like every shipped migration.
func migrateTo8(ctx context.Context, tx *sql.Tx, _ migrationEnv) error {
	return execAll(ctx, tx, schemaV8)
}

var schemaV8 = []string{
	`UPDATE runs SET resource_kind = NULL, resource_id = NULL
		WHERE resource_id IS NOT NULL
		AND NOT (resource_kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = runs.resource_id))
		AND NOT (resource_kind = 'playlist' AND EXISTS (SELECT 1 FROM playlists p WHERE p.id = runs.resource_id))
		AND NOT EXISTS (SELECT 1 FROM pending_adds p WHERE p.kind = runs.resource_kind AND p.catalog_id = runs.resource_id)
		AND NOT (resource_kind = 'album' AND EXISTS (SELECT 1 FROM pending_adds p
			WHERE p.kind = 'song' AND p.album_hint = runs.resource_id))`,
}
