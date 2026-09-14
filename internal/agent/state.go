package agent

import (
	"fmt"
	"sort"
)

type Profile struct {
	Name        string
	MemoryMB    int
	Compute     int
	Size        int
	LegalStarts map[int]bool
}

type Decision struct {
	Key     string `json:"key"`
	Backend string `json:"backend"`
	Node    string `json:"node"`
	GPU     string `json:"gpu"`
	Profile string `json:"profile"`
	Start   int    `json:"start"`
	Size    int    `json:"size"`
}

type State struct {
	Profiles map[string]Profile
	Slots    int
	Active   map[string]Decision
}

func H100State() *State {
	profiles := []Profile{
		{Name: "1g.10gb", MemoryMB: 10240, Compute: 14, Size: 1, LegalStarts: starts(0, 1, 2, 3, 4, 5, 6)},
		{Name: "2g.20gb", MemoryMB: 20480, Compute: 28, Size: 2, LegalStarts: starts(0, 2, 4)},
		{Name: "3g.40gb", MemoryMB: 40960, Compute: 42, Size: 3, LegalStarts: starts(0, 4)},
		{Name: "7g.80gb", MemoryMB: 81920, Compute: 100, Size: 7, LegalStarts: starts(0)},
	}
	s := &State{Profiles: map[string]Profile{}, Slots: 7, Active: map[string]Decision{}}
	for _, p := range profiles {
		s.Profiles[p.Name] = p
	}
	return s
}

func starts(v ...int) map[int]bool {
	m := map[int]bool{}
	for _, x := range v {
		m[x] = true
	}
	return m
}

// Replace validates a complete scheduler-owned allocation snapshot before
// making it visible. The fake agent never chooses or changes a placement.
func (s *State) Replace(decisions []Decision) error {
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Key < decisions[j].Key })
	occupied := map[string][]string{}
	next := map[string]Decision{}
	for _, d := range decisions {
		p, ok := s.Profiles[d.Profile]
		if !ok {
			return fmt.Errorf("%s: unknown profile %q", d.Key, d.Profile)
		}
		if d.Size == 0 {
			d.Size = p.Size
		}
		if d.Size != p.Size || !p.LegalStarts[d.Start] || d.Start+d.Size > s.Slots {
			return fmt.Errorf("%s: illegal %s placement start=%d size=%d", d.Key, d.Profile, d.Start, d.Size)
		}
		gpu := d.Node + "/" + d.GPU
		if occupied[gpu] == nil {
			occupied[gpu] = make([]string, s.Slots)
		}
		for slot := d.Start; slot < d.Start+d.Size; slot++ {
			if owner := occupied[gpu][slot]; owner != "" {
				return fmt.Errorf("%s overlaps %s on %s slot %d", d.Key, owner, gpu, slot)
			}
			occupied[gpu][slot] = d.Key
		}
		next[d.Key] = d
	}
	s.Active = next
	return nil
}
