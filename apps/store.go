package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Store persists install records. FileStore is the default; a host that
// keeps state elsewhere (for example in its own database) can supply
// another implementation.
type Store interface {
	ListApps(ctx context.Context) ([]Record, error)
	// GetApp returns ErrNotInstalled when id has no record.
	GetApp(ctx context.Context, id string) (*Record, error)
	PutApp(ctx context.Context, record Record) error
	// DeleteApp removes id's record; removing a missing record succeeds.
	DeleteApp(ctx context.Context, id string) error
}

// recordFile holds an app's install record inside its directory.
const recordFile = "app.json"

// FileStore keeps each record as <root>/<id>/app.json beside the app's
// compose files, so an app directory is complete on its own: it can be
// backed up, moved, or driven with the plain compose CLI.
type FileStore struct{ root string }

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

func (s *FileStore) path(id string) string { return filepath.Join(s.root, id, recordFile) }

func (s *FileStore) ListApps(ctx context.Context) ([]Record, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		record, err := s.GetApp(ctx, entry.Name())
		if err != nil {
			continue // a directory without a record is not an installed app
		}
		out = append(out, *record)
	}
	return out, nil
}

func (s *FileStore) GetApp(_ context.Context, id string) (*Record, error) {
	data, err := os.ReadFile(s.path(id))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path(id), err)
	}
	r.ID = id // the directory name is authoritative
	return &r, nil
}

func (s *FileStore) PutApp(_ context.Context, record Record) error {
	if err := os.MkdirAll(filepath.Join(s.root, record.ID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(record.ID), append(data, '\n'), 0o600)
}

func (s *FileStore) DeleteApp(_ context.Context, id string) error {
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
