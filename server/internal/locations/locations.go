package locations

import (
	"os"

	"github.com/starsdaisuki/starnexus/server/internal/db"
	"gopkg.in/yaml.v3"
)

type fileFormat struct {
	Nodes []Override `yaml:"nodes"`
}

type Override struct {
	ID        string  `yaml:"id"`
	Latitude  float64 `yaml:"latitude"`
	Longitude float64 `yaml:"longitude"`
	Note      string  `yaml:"note"`
}

type Store struct {
	byNodeID map[string]Override
}

func Load(path string) (*Store, error) {
	if path == "" {
		return &Store{byNodeID: map[string]Override{}}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{byNodeID: map[string]Override{}}, nil
		}
		return nil, err
	}

	var parsed fileFormat
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	store := &Store{byNodeID: make(map[string]Override, len(parsed.Nodes))}
	for _, item := range parsed.Nodes {
		if item.ID == "" {
			continue
		}
		store.byNodeID[item.ID] = item
	}
	return store, nil
}

func (s *Store) ApplyReport(req *db.ReportRequest) bool {
	if s == nil || req == nil {
		return false
	}

	override, ok := s.byNodeID[req.NodeID]
	if !ok {
		return false
	}

	req.Latitude = override.Latitude
	req.Longitude = override.Longitude
	req.LocationSource = "manual_override"
	return true
}

func (s *Store) DBOverrides() []db.NodeLocationOverride {
	if s == nil || len(s.byNodeID) == 0 {
		return nil
	}

	overrides := make([]db.NodeLocationOverride, 0, len(s.byNodeID))
	for _, item := range s.byNodeID {
		overrides = append(overrides, db.NodeLocationOverride{
			NodeID:         item.ID,
			Latitude:       item.Latitude,
			Longitude:      item.Longitude,
			LocationSource: "manual_override",
		})
	}
	return overrides
}
