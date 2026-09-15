package controlplane

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

type fakeSchedulerAPI struct {
	mu     sync.Mutex
	claims map[string]bool
}

func (f *fakeSchedulerAPI) Apply(_ context.Context, object any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := object.(map[string]any)
	if obj["kind"] == "ResourceClaim" {
		metadata := obj["metadata"].(map[string]any)
		if f.claims == nil {
			f.claims = map[string]bool{}
		}
		f.claims[metadata["name"].(string)] = true
	}
	return nil
}

func (f *fakeSchedulerAPI) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) >= 2 && args[0] == "get" && args[1] == "resourceclaims" {
		var names []string
		for name, active := range f.claims {
			if active {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		items := []any{}
		for i, name := range names {
			claim := map[string]any{"metadata": map[string]any{"name": name, "namespace": "default"}}
			// The fake scheduler exposes only one whole-GPU allocation at a time.
			if i == 0 {
				claim["status"] = map[string]any{"allocation": map[string]any{"devices": map[string]any{"results": []any{map[string]any{"driver": "gpu.nvidia.com", "pool": "worker-a", "device": "gpu0-mig-7g80gb-0", "request": "gpu"}}}}}
			}
			items = append(items, claim)
		}
		return json.Marshal(map[string]any{"items": items})
	}
	if len(args) >= 3 && args[0] == "delete" && args[1] == "resourceclaim" {
		f.claims[args[2]] = false
	}
	return []byte(`{"items":[]}`), nil
}

func TestRunAdvancesVirtualTimeWhileUsingSchedulerPlacements(t *testing.T) {
	c := config.Config{
		Cluster:  config.Cluster{Nodes: 1, GPUsPerNode: 1, Model: "H100-80GB", GPCPerGPU: 7, MemoryGB: 80, Profiles: config.H100Profiles()},
		Workload: config.Workload{Seed: 7},
		Limits:   config.Limits{SchedulingTimeoutMS: 1000},
	}
	b := config.Backend{Name: "dra-real", Kind: "nvidia-dra", Mode: "control-plane", Parameters: map[string]string{"nodes": "worker-a", "settleMS": "5", "pollMS": "1"}}
	jobs := []model.Job{
		{ID: "job-a", ArrivalMS: 0, ComputeCoreMS: 10000, MemoryMB: 81920, MinComputePercent: 100},
		{ID: "job-b", ArrivalMS: 0, ComputeCoreMS: 10000, MemoryMB: 81920, MinComputePercent: 100},
	}
	r, err := Run(context.Background(), c, b, jobs, &fakeSchedulerAPI{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Completed != 2 || r.Summary.MakespanMS != 200 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if r.Jobs[0].StartMS != 0 || r.Jobs[1].StartMS != 100 || r.Jobs[1].WaitMS != 100 {
		t.Fatalf("unexpected virtual lifecycle: %+v", r.Jobs)
	}
	if r.Jobs[0].Profile != "7g.80gb" || r.Jobs[0].Node != "worker-a" {
		t.Fatalf("scheduler placement was not normalized: %+v", r.Jobs[0])
	}
}

func TestJobFromKey(t *testing.T) {
	if got := jobFromKey("namespace/job-1/0"); got != "job-1" {
		t.Fatalf("got %q", got)
	}
}

func TestRegistrationWait(t *testing.T) {
	hami := config.Backend{Kind: "hami", Parameters: map[string]string{}}
	if got := registrationWait(hami); got != 16*time.Second {
		t.Fatalf("HAMi default registration wait = %s", got)
	}
	hami.Parameters["registrationWaitMS"] = "0"
	if got := registrationWait(hami); got != 0 {
		t.Fatalf("explicit registration wait = %s", got)
	}
	dra := config.Backend{Kind: "nvidia-dra", Parameters: map[string]string{}}
	if got := registrationWait(dra); got != 0 {
		t.Fatalf("DRA registration wait = %s", got)
	}
}
