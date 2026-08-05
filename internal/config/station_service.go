package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GetStation returns a copy of station config (thread-safe).
func (s *Service) GetStation(id string) (*Station, error) {
	if err := ValidateStationID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.stations[id]
	if !exists {
		return nil, fmt.Errorf("station not found: %s", id)
	}
	copy := *st
	return &copy, nil
}

// ListStations returns copies of all stations sorted by ID.
func (s *Service) ListStations() []Station {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stations := make([]Station, 0, len(s.stations))
	for _, st := range s.stations {
		stations = append(stations, *st)
	}
	sort.Slice(stations, func(i, j int) bool {
		return stations[i].ID < stations[j].ID
	})
	return stations
}

// AddStation adds a new station and returns the persisted copy (including assigned ID).
func (s *Service) AddStation(st Station) (Station, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(st.Name) == "" {
		return Station{}, fmt.Errorf("station name is required")
	}
	if st.ID == "" {
		st.ID = s.allocateUniqueStationIDLocked(st.Name)
	}
	NormalizeStationDefaults(&st)
	if err := ValidateStation(st); err != nil {
		return Station{}, err
	}
	if _, exists := s.stations[st.ID]; exists {
		return Station{}, fmt.Errorf("station already exists: %s", st.ID)
	}

	if err := s.saveStationFile(st); err != nil {
		return Station{}, err
	}

	copy := st
	s.stations[st.ID] = &copy
	s.notifyListeners(ConfigEvent{Type: "station_added", StationID: st.ID})
	return st, nil
}

// UpdateStation updates an existing station atomically.
func (s *Service) UpdateStation(id string, fn func(*Station) error) error {
	if err := ValidateStationID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.stations[id]
	if !exists {
		return fmt.Errorf("station not found: %s", id)
	}

	updated := *st
	if err := fn(&updated); err != nil {
		return err
	}
	updated.ID = id
	NormalizeStationDefaults(&updated)
	if err := ValidateStation(updated); err != nil {
		return err
	}

	if err := s.saveStationFile(updated); err != nil {
		return err
	}

	s.stations[id] = &updated
	s.notifyListeners(ConfigEvent{Type: "station_updated", StationID: id})
	return nil
}

// DeleteStation removes a station atomically.
func (s *Service) DeleteStation(id string) error {
	if err := ValidateStationID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.stations[id]; !exists {
		return fmt.Errorf("station not found: %s", id)
	}

	path, err := StationConfigPath(s.baseDir, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete station file: %w", err)
	}

	delete(s.stations, id)
	s.notifyListeners(ConfigEvent{Type: "station_deleted", StationID: id})
	return nil
}

func (s *Service) saveStationFile(st Station) error {
	path, err := StationConfigPath(s.baseDir, st.ID)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not backup station config: %v\n", err)
		}
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write station config: %w", err)
	}

	stationsDir := filepath.Join(s.baseDir, "stations")
	if err := os.Chmod(stationsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set stations directory permissions: %v\n", err)
	}
	return nil
}

func (s *Service) loadStationsLocked() error {
	stationsDir := filepath.Join(s.baseDir, "stations")
	entries, err := os.ReadDir(stationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read stations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		stPath := filepath.Join(stationsDir, entry.Name())
		data, err := os.ReadFile(stPath)
		if err != nil {
			return fmt.Errorf("read station file %s: %w", entry.Name(), err)
		}
		var st Station
		if err := json.Unmarshal(data, &st); err != nil {
			return fmt.Errorf("parse station file %s: %w", entry.Name(), err)
		}
		NormalizeStationDefaults(&st)
		if err := ValidateStation(st); err != nil {
			return fmt.Errorf("station file %s: %w", entry.Name(), err)
		}
		if _, err := StationConfigPath(s.baseDir, st.ID); err != nil {
			return fmt.Errorf("station file %s: %w", entry.Name(), err)
		}
		s.stations[st.ID] = &st
	}
	return nil
}
