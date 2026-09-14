package adapter

import (
	"strings"
	"testing"

	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

func TestNativeObjects(t *testing.T) {
	j := model.Job{ID: "job-1", Profile: "2g.20gb"}
	for _, tc := range []struct{ kind, want string }{{"hami", "hami.io/vgpu-mode"}, {"nvidia-dra", "mig.nvidia.com"}} {
		a := New(config.Backend{Name: tc.kind, Kind: tc.kind, Policy: "binpack"})
		b, err := a.Objects(j)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s object missing %q: %s", tc.kind, tc.want, b)
		}
	}
}

func TestDRAResourceBoundUsesQuantityComparison(t *testing.T) {
	j := model.Job{ID: "job-2", ComputeCoreMS: 1000, MemoryMB: 18432, MinComputePercent: 20}
	b, err := New(config.Backend{Name: "dra", Kind: "nvidia-dra"}).Objects(j)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), ".compareTo(quantity(") {
		t.Fatalf("resource selector does not use Kubernetes Quantity comparison: %s", b)
	}
}

func TestPolicies(t *testing.T) {
	cs := []Candidate{{Node: "a", FreeGPC: 6}, {Node: "b", FreeGPC: 2}}
	j := model.Job{}
	p := model.Profile{}
	x, _ := New(config.Backend{Name: "b", Kind: "baseline", Policy: "binpack"}).Choose(j, p, cs)
	if x.Node != "b" {
		t.Fatalf("binpack chose %s", x.Node)
	}
	x, _ = New(config.Backend{Name: "s", Kind: "baseline", Policy: "spread"}).Choose(j, p, cs)
	if x.Node != "a" {
		t.Fatalf("spread chose %s", x.Node)
	}
}
