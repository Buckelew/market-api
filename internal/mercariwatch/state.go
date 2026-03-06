package mercariwatch

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type StateEntry struct {
	LastSeenAt     string `json:"last_seen_at,omitempty"`
	LastPriceCents int    `json:"last_price_cents,omitempty"`
	LastGeneralKey string `json:"last_general_key,omitempty"`
	LastDealKey    string `json:"last_deal_key,omitempty"`
}

type StateStore struct {
	Entries map[string]StateEntry `json:"entries"`
	Order   []string              `json:"order"`
}

func LoadState(path string) (*StateStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &StateStore{Entries: map[string]StateEntry{}, Order: []string{}}, nil
		}
		return nil, err
	}

	var state StateStore
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Entries == nil {
		state.Entries = map[string]StateEntry{}
	}
	if state.Order == nil {
		state.Order = []string{}
	}
	return &state, nil
}

func SaveState(path string, state *StateStore) error {
	if state == nil {
		state = &StateStore{Entries: map[string]StateEntry{}, Order: []string{}}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func TrimState(state *StateStore, maxEntries int) {
	if state == nil || maxEntries <= 0 || len(state.Order) <= maxEntries {
		return
	}

	seen := make(map[string]struct{}, len(state.Order))
	deduped := make([]string, 0, len(state.Order))
	for i := len(state.Order) - 1; i >= 0; i-- {
		id := state.Order[i]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}

	trimmed := make([]string, 0, minInt(maxEntries, len(deduped)))
	for i := len(deduped) - 1; i >= 0 && len(trimmed) < maxEntries; i-- {
		trimmed = append(trimmed, deduped[i])
	}

	state.Order = trimmed
	keep := make(map[string]struct{}, len(trimmed))
	for _, id := range trimmed {
		keep[id] = struct{}{}
	}
	for id := range state.Entries {
		if _, ok := keep[id]; !ok {
			delete(state.Entries, id)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
