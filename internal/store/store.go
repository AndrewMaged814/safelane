// Package store persists Release records to disk, one JSON file per
// release, so a release ID survives process restarts and later commands
// (`safelane proof`, #50's decision step) can load it back.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// FileStore persists Release records as one JSON file per release under Dir.
//
// Writes are atomic: the record is written to a temp file in Dir and then
// renamed into place, so a crash mid-write cannot leave a corrupt or
// partial record for a later read to trip over. Dir is created on first
// Save if it does not already exist.
type FileStore struct {
	Dir string
}

func (s *FileStore) path(id release.ReleaseID) string {
	return filepath.Join(s.Dir, string(id)+".json")
}

// Save persists r. It refuses to overwrite an existing record: a release ID
// is assigned once and never reused, so a second Save for the same ID
// means something upstream minted a duplicate ID, which is a defect worth
// surfacing rather than silently clobbering the earlier record.
func (s *FileStore) Save(r *release.Release) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("store: could not create %s: %w", s.Dir, err)
	}
	dest := s.path(r.ID)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("store: a release record already exists at %s", dest)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("store: could not encode release %s: %w", r.ID, err)
	}

	tmp, err := os.CreateTemp(s.Dir, "."+string(r.ID)+".*.tmp")
	if err != nil {
		return fmt.Errorf("store: could not create a temp file in %s: %w", s.Dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: could not write %s: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: could not close the temp file for %s: %w", dest, err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("store: could not finalize %s: %w", dest, err)
	}
	return nil
}

// Load reads a persisted Release by ID. Every invariant [release.NewRelease]
// enforces is re-checked via [release.Release.UnmarshalJSON], so a corrupt
// or tampered record is rejected rather than trusted.
func (s *FileStore) Load(id release.ReleaseID) (*release.Release, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("store: no release record for %s", id)
		}
		return nil, fmt.Errorf("store: could not read the record for %s: %w", id, err)
	}
	var r release.Release
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
