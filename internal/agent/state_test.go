package agent

import "testing"

func TestStateRejectsIllegalAndOverlappingPlacements(t *testing.T) {
	s := H100State()
	if err := s.Replace([]Decision{{Key: "bad", Node: "n", GPU: "0", Profile: "2g.20gb", Start: 1, Size: 2}}); err == nil {
		t.Fatal("accepted illegal 2g start")
	}
	err := s.Replace([]Decision{
		{Key: "a", Node: "n", GPU: "0", Profile: "3g.40gb", Start: 0, Size: 3},
		{Key: "b", Node: "n", GPU: "0", Profile: "2g.20gb", Start: 2, Size: 2},
	})
	if err == nil {
		t.Fatal("accepted overlapping placements")
	}
}

func TestStateAcceptsLegalLayoutAndReleaseSnapshot(t *testing.T) {
	s := H100State()
	if err := s.Replace([]Decision{
		{Key: "a", Node: "n", GPU: "0", Profile: "3g.40gb", Start: 0, Size: 3},
		{Key: "b", Node: "n", GPU: "0", Profile: "3g.40gb", Start: 4, Size: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace([]Decision{{Key: "b", Node: "n", GPU: "0", Profile: "3g.40gb", Start: 4, Size: 3}}); err != nil || len(s.Active) != 1 {
		t.Fatalf("release snapshot failed: %v %#v", err, s.Active)
	}
}
