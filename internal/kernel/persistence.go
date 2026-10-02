package kernel

import (
	"os"
	"path/filepath"
)

// PersistenceManager owns the on-disk data root. Every service gets a
// dedicated subdirectory. Nothing here is in-memory-only.
type PersistenceManager struct {
	root string
}

func NewPersistenceManager(root string) (*PersistenceManager, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &PersistenceManager{root: root}, nil
}

func (p *PersistenceManager) Root() string { return p.root }

func (p *PersistenceManager) ServiceDir(service string) (string, error) {
	dir := filepath.Join(p.root, service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Reset deletes the entire data root. Only called by `azlocal reset`.
func (p *PersistenceManager) Reset() error {
	return os.RemoveAll(p.root)
}
