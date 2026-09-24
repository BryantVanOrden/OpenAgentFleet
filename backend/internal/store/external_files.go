package store

import "context"

// ExternalFile is a file an external agent shared, as the catalog has it now.
type ExternalFile struct {
	ItemID  string
	Path    string
	Version int
	Content string
	By      string // who wrote the current version
}

// RecordExternalFile notes that an agent has this version of a file in its
// folder, because it shared it or was just given it.
func (s *Store) RecordExternalFile(ctx context.Context, instanceID, itemID, path string, version int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO external_files(instance_id,item_id,path,version) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (instance_id,item_id) DO UPDATE SET path=EXCLUDED.path, version=EXCLUDED.version`,
		instanceID, itemID, path, version)
	return norm(err)
}

// ChangedExternalFiles are the agent's shared files that someone else has
// published a newer version of since it last had them.
func (s *Store) ChangedExternalFiles(ctx context.Context, instanceID string) ([]ExternalFile, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT w.id, f.path, w.version, w.content, COALESCE(NULLIF(w.created_by_name,''),'the operator')
		   FROM external_files f JOIN work_items w ON w.id = f.item_id
		  WHERE f.instance_id = $1 AND w.version > f.version AND COALESCE(w.created_by,'') <> $1
		  ORDER BY w.updated_at`, instanceID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	var out []ExternalFile
	for rows.Next() {
		var f ExternalFile
		if err := rows.Scan(&f.ItemID, &f.Path, &f.Version, &f.Content, &f.By); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
