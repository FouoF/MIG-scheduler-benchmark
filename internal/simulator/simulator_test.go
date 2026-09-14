package simulator

import (
	"testing"

	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

func tiny() config.Config {
	return config.Config{Name: "test", Cluster: config.Cluster{Nodes: 1, GPUsPerNode: 1, Model: "H100-80GB", GPCPerGPU: 7, MemoryGB: 80, Profiles: config.H100Profiles()}, Backends: []config.Backend{{Name: "first", Kind: "baseline", Mode: "simulated", Policy: "first-fit"}}, Costs: config.Costs{CreateMS: 10, DeleteMS: 5, ReconfigureMS: 20}, Workload: config.Workload{Seed: 1}}
}

func TestQueueReleaseAndNoLeak(t *testing.T) {
	c := tiny()
	jobs := []model.Job{{ID: "a", ArrivalMS: 0, DurationMS: 100, Profile: "7g.80gb"}, {ID: "b", ArrivalMS: 1, DurationMS: 50, Profile: "7g.80gb"}}
	r, err := Run(c, c.Backends[0], jobs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Completed != 2 || r.Summary.MIGCreates != 2 || r.Summary.MIGDeletes != 2 {
		t.Fatalf("bad summary: %+v", r.Summary)
	}
	if r.Jobs[1].StartMS < r.Jobs[0].FinishMS {
		t.Fatalf("queued job started before release: %+v", r.Jobs)
	}
}

func TestFragmentedRequestRecorded(t *testing.T) {
	c := tiny()
	jobs := []model.Job{{ID: "a", ArrivalMS: 0, DurationMS: 100, Profile: "3g.40gb"}, {ID: "b", ArrivalMS: 0, DurationMS: 100, Profile: "3g.40gb"}, {ID: "c", ArrivalMS: 1, DurationMS: 10, Profile: "2g.20gb"}}
	r, err := Run(c, c.Backends[0], jobs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Completed != 3 {
		t.Fatalf("not all completed: %+v", r.Summary)
	}
}
