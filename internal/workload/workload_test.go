package workload

import (
	"math"
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

func TestOfferedLoadDerivesArrivalRateWithoutPrefill(t *testing.T) {
	c := config.Config{
		Cluster:  config.Cluster{Nodes: 2, GPUsPerNode: 4, GPCPerGPU: 7, Profiles: config.H100Profiles()},
		Workload: config.Workload{Seed: 1, Jobs: 100, Model: "poisson", TargetOfferedLoad: 1.1, DurationMedianMS: 600000, DurationSigma: .1},
	}
	jobs, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].ArrivalMS <= 0 {
		t.Fatalf("first job should arrive through the configured process, got %d", jobs[0].ArrivalMS)
	}
	mean, err := arrivalMeanMS(c, jobs)
	if err != nil {
		t.Fatal(err)
	}
	var occupied float64
	for _, j := range jobs {
		p, ok := smallestProfileForMemory(c.Cluster.Profiles, j.MemoryMB)
		if !ok {
			t.Fatal("generated infeasible job")
		}
		runtime := (j.ComputeCoreMS + int64(p.ComputePercent) - 1) / int64(p.ComputePercent)
		occupied += float64(p.GPC) * float64(runtime)
	}
	load := occupied / float64(len(jobs)*2*4*7) / mean
	if math.Abs(load-1.1) > 1e-9 {
		t.Fatalf("derived offered load=%f, want 1.1", load)
	}
}
