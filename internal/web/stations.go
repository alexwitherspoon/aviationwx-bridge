package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listStations(w, r)
	case http.MethodPost:
		s.addStation(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listStations(w http.ResponseWriter, r *http.Request) {
	stations := s.configService.ListStations()
	result := make([]map[string]interface{}, 0, len(stations))
	for _, st := range stations {
		result = append(result, stationToMap(st))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) addStation(w http.ResponseWriter, r *http.Request) {
	var st config.Station
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(st.Name) == "" {
		http.Error(w, "Display name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(st.Type) == "" {
		st.Type = config.StationTypeDavisWeatherLinkLive
	}
	if st.Type != config.StationTypeDavisWeatherLinkLive {
		http.Error(w, fmt.Sprintf("unsupported station type %q", st.Type), http.StatusBadRequest)
		return
	}
	// Id is allocated from the display name (client-supplied id ignored on create).
	st.ID = ""

	added, err := s.configService.AddStation(st)
	if err != nil {
		http.Error(w, "Failed to add station: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Info("Station added via API", "station", added.ID, "type", added.Type)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(stationToMap(added))
}

func (s *Server) handleStation(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/stations/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Station ID required", http.StatusBadRequest)
		return
	}
	stationID := parts[0]
	if err := config.ValidateStationID(stationID); err != nil {
		http.Error(w, "Invalid station ID", http.StatusBadRequest)
		return
	}
	if len(parts) > 1 && parts[1] != "" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getStation(w, r, stationID)
	case http.MethodPut:
		s.updateStation(w, r, stationID)
	case http.MethodDelete:
		s.deleteStation(w, r, stationID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getStation(w http.ResponseWriter, r *http.Request, stationID string) {
	st, err := s.configService.GetStation(stationID)
	if err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stationToMap(*st))
}

func (s *Server) updateStation(w http.ResponseWriter, r *http.Request, stationID string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.configService.GetStation(stationID); err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	err := s.configService.UpdateStation(stationID, func(st *config.Station) error {
		if v, ok := raw["name"]; ok {
			var name string
			if err := json.Unmarshal(v, &name); err != nil {
				return fmt.Errorf("name: %w", err)
			}
			if strings.TrimSpace(name) != "" {
				st.Name = name
			}
		}
		if v, ok := raw["type"]; ok {
			var typ string
			if err := json.Unmarshal(v, &typ); err != nil {
				return fmt.Errorf("type: %w", err)
			}
			if strings.TrimSpace(typ) != "" {
				st.Type = typ
			}
		}
		if v, ok := raw["enabled"]; ok {
			var enabled bool
			if err := json.Unmarshal(v, &enabled); err != nil {
				return fmt.Errorf("enabled: %w", err)
			}
			st.Enabled = enabled
		}
		if v, ok := raw["host"]; ok {
			var host string
			if err := json.Unmarshal(v, &host); err != nil {
				return fmt.Errorf("host: %w", err)
			}
			st.Host = host
		}
		if v, ok := raw["poll_interval_seconds"]; ok {
			var poll int
			if err := json.Unmarshal(v, &poll); err != nil {
				return fmt.Errorf("poll_interval_seconds: %w", err)
			}
			if poll > 0 {
				st.PollIntervalSeconds = poll
			}
		}
		// txid is presence-sensitive: omit keeps current; null clears; number sets.
		if v, ok := raw["txid"]; ok {
			if string(v) == "null" {
				st.Txid = nil
			} else {
				var txid int
				if err := json.Unmarshal(v, &txid); err != nil {
					return fmt.Errorf("txid: %w", err)
				}
				st.Txid = &txid
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Failed to update station: "+err.Error(), http.StatusBadRequest)
		return
	}

	st, _ := s.configService.GetStation(stationID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stationToMap(*st))
}

func (s *Server) deleteStation(w http.ResponseWriter, r *http.Request, stationID string) {
	if err := s.configService.DeleteStation(stationID); err != nil {
		http.Error(w, "Failed to delete station: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func stationToMap(st config.Station) map[string]interface{} {
	m := map[string]interface{}{
		"id":                    st.ID,
		"name":                  st.Name,
		"type":                  st.Type,
		"enabled":               st.Enabled,
		"host":                  st.Host,
		"poll_interval_seconds": st.PollIntervalSeconds,
	}
	if st.Txid != nil {
		m["txid"] = *st.Txid
	}
	return m
}
