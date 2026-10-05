package arm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
)

const (
	DefaultSubscriptionID = "local-sub"
	DefaultTenantID       = "local"
	DefaultSubscriptionName = "Local Subscription"
)

type Store struct {
	root string
	mu   sync.RWMutex
}

func NewStore(persist *kernel.PersistenceManager) (*Store, error) {
	root, err := persist.ServiceDir("arm")
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"subscriptions", "resourcegroups", "resources"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}
	s := &Store{root: root}
	if err := s.ensureDefaultSubscription(); err != nil {
		return nil, err
	}
	return s, nil
}

// ---- subscriptions --------------------------------------------------------

func (s *Store) subPath(id string) string {
	return filepath.Join(s.root, "subscriptions", id+".json")
}

func (s *Store) ensureDefaultSubscription() error {
	if _, ok := s.ReadSubscription(DefaultSubscriptionID); ok {
		return nil
	}
	sub := Subscription{
		ID:               "/subscriptions/" + DefaultSubscriptionID,
		SubscriptionID:   DefaultSubscriptionID,
		TenantID:         DefaultTenantID,
		DisplayName:      DefaultSubscriptionName,
		State:            "Enabled",
		AuthorizationSrc: "RoleBased",
		Policies: &SubscriptionPol{
			LocationPlacementID: "Public_2014-09-01",
			QuotaID:             "PayAsYouGo_2014-09-01",
			SpendingLimit:       "Off",
		},
	}
	return s.WriteSubscription(sub)
}

func (s *Store) ReadSubscription(id string) (Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.subPath(id))
	if err != nil {
		return Subscription{}, false
	}
	var sub Subscription
	if err := json.Unmarshal(raw, &sub); err != nil {
		return Subscription{}, false
	}
	return sub, true
}

func (s *Store) WriteSubscription(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(s.subPath(sub.SubscriptionID), sub)
}

func (s *Store) ListSubscriptions() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.root, "subscriptions")
	entries, _ := os.ReadDir(dir)
	var out []Subscription
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sub Subscription
		if json.Unmarshal(raw, &sub) == nil {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubscriptionID < out[j].SubscriptionID })
	return out
}

// ---- resource groups ------------------------------------------------------

func (s *Store) rgDir(sub string) string {
	return filepath.Join(s.root, "resourcegroups", sub)
}

func (s *Store) rgPath(sub, name string) string {
	return filepath.Join(s.rgDir(sub), name+".json")
}

func (s *Store) ReadResourceGroup(sub, name string) (ResourceGroup, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.rgPath(sub, name))
	if err != nil {
		return ResourceGroup{}, false
	}
	var rg ResourceGroup
	if err := json.Unmarshal(raw, &rg); err != nil {
		return ResourceGroup{}, false
	}
	return rg, true
}

func (s *Store) WriteResourceGroup(rg ResourceGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.rgDir(subFromID(rg.ID)), 0o755); err != nil {
		return err
	}
	return writeJSONFile(s.rgPath(subFromID(rg.ID), rg.Name), rg)
}

func (s *Store) DeleteResourceGroup(sub, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Delete all resources in the RG first.
	resDir := filepath.Join(s.root, "resources", sub, name)
	_ = os.RemoveAll(resDir)
	return os.Remove(s.rgPath(sub, name))
}

func (s *Store) ListResourceGroups(sub string) []ResourceGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := s.rgDir(sub)
	entries, _ := os.ReadDir(dir)
	var out []ResourceGroup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rg ResourceGroup
		if json.Unmarshal(raw, &rg) == nil {
			out = append(out, rg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- resources ------------------------------------------------------------

func (s *Store) resPath(sub, rg, ns, rtype, name string) string {
	if rg == "" {
		return filepath.Join(s.root, "resources", sub, "_subscription", ns, rtype, name+".json")
	}
	return filepath.Join(s.root, "resources", sub, rg, ns, rtype, name+".json")
}

func (s *Store) ReadResource(sub, rg, ns, rtype, name string) (StoredResource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.resPath(sub, rg, ns, rtype, name))
	if err != nil {
		return StoredResource{}, false
	}
	var res StoredResource
	if err := json.Unmarshal(raw, &res); err != nil {
		return StoredResource{}, false
	}
	return res, true
}

func (s *Store) WriteResource(res StoredResource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.resPath(res.Subscription, res.ResourceGroup, res.Namespace, res.ResourceType, res.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSONFile(path, res)
}

func (s *Store) DeleteResource(sub, rg, ns, rtype, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.resPath(sub, rg, ns, rtype, name))
}

// listResources walks either a specific RG directory or all RGs under a sub.
func (s *Store) ListResources(sub, rg string) []StoredResource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	base := filepath.Join(s.root, "resources", sub)
	if rg != "" {
		base = filepath.Join(base, rg)
	}
	var out []StoredResource
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var res StoredResource
		if json.Unmarshal(raw, &res) == nil {
			out = append(out, res)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Body.ID < out[j].Body.ID })
	return out
}

// ---- helpers --------------------------------------------------------------

func subFromID(id string) string {
	// /subscriptions/{sub}/resourceGroups/{rg}
	parts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	if len(parts) >= 2 && parts[0] == "subscriptions" {
		return parts[1]
	}
	return DefaultSubscriptionID
}

func writeJSONFile(path string, v any) error {
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var _ = fmt.Sprintf
var _ = time.Now
