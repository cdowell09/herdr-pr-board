package reviewmemory

import "strings"

// Snapshot reads all local outcomes and live claims in one transaction.
// The returned data belongs to the caller. It does not reserve review slots.
type Snapshot struct {
	Runs   []Run
	Active map[string]bool
}

func (s *Store) Snapshot() (snapshot Snapshot, err error) {
	err = s.transaction(func(h *history) error {
		active, err := s.activeClaims(h)
		if err != nil {
			return err
		}
		snapshot = Snapshot{Runs: h.Runs, Active: active}
		return nil
	})
	return snapshot, err
}

func (s Snapshot) ReviewStatus(id Identity) error {
	if err := ValidateIdentity(id); err != nil {
		return err
	}
	id.Repository = strings.ToLower(id.Repository)
	return historyStatus(s.Runs, s.Active, id)
}
