package workload

import (
	"reflect"
	"testing"

	"github.com/dynamia-ai/migbench/internal/config"
)

func TestGenerateDeterministic(t *testing.T) {
	c := config.Config{Cluster: config.Cluster{Nodes: 1, GPUsPerNode: 1, Model: "H100-80GB", GPCPerGPU: 7, MemoryGB: 80, Profiles: config.H100Profiles()}, Backends: []config.Backend{{Name: "x", Kind: "baseline", Mode: "simulated"}}, Workload: config.Workload{Seed: 42, Jobs: 20, Model: "poisson", ArrivalMeanMS: 10, DurationMedianMS: 100, DurationSigma: .5}}
	a, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed generated different traces")
	}
}

func TestAdversarialOrdersSmallThenLarge(t *testing.T) {
	c := config.Config{Cluster: config.Cluster{Profiles: config.H100Profiles()}, Workload: config.Workload{Seed: 1, Jobs: 4, Model: "adversarial", ArrivalMeanMS: 1, DurationMedianMS: 1, DurationSigma: .1}}
	j, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	if j[0].MemoryMB != 10240 || j[3].MemoryMB != 81920 || j[0].Profile != "" || j[3].Profile != "" {
		t.Fatalf("unexpected profiles: %#v", j)
	}
}

func TestPrefillOverridesAdversarialPhase(t *testing.T) {
	c := config.Config{Cluster: config.Cluster{Profiles: config.H100Profiles()}, Workload: config.Workload{Seed: 1, Jobs: 8, PrefillJobs: 7, Model: "adversarial", ArrivalMeanMS: 1, DurationMedianMS: 1, DurationSigma: .1}}
	jobs, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if jobs[i].MemoryMB != 10240 || jobs[i].MinComputePercent != 14 {
			t.Fatalf("prefill job %d was overridden: %+v", i, jobs[i])
		}
	}
	if jobs[7].MemoryMB != 81920 {
		t.Fatalf("post-prefill adversarial job is not large: %+v", jobs[7])
	}
}
