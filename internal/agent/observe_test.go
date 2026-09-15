package agent

import (
	"context"
	"testing"
)

func TestHasAllocationMatchesExactLockOwner(t *testing.T) {
	a := &Agent{State: H100State()}
	a.State.Active["team-a/job-a/0"] = Decision{Key: "team-a/job-a/0"}
	if !a.hasAllocation("team-a", "job-a") {
		t.Fatal("expected exact lock owner to match")
	}
	if a.hasAllocation("team-a", "job") || a.hasAllocation("team-b", "job-a") {
		t.Fatal("partial or different lock owner must not match")
	}
}

type mutexAPI struct {
	node  string
	calls [][]string
}

func (m *mutexAPI) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	m.calls = append(m.calls, args)
	return []byte(m.node), nil
}

func (*mutexAPI) Apply(context.Context, any) error { return nil }

type observeAPI struct{ list string }

func (o *observeAPI) Run(context.Context, []byte, ...string) ([]byte, error) {
	return []byte(o.list), nil
}

func (*observeAPI) Apply(context.Context, any) error { return nil }

func TestObserveHAMiIgnoresTerminatingPods(t *testing.T) {
	api := &observeAPI{list: `{"items":[{"metadata":{"name":"old","namespace":"default","deletionTimestamp":"2026-09-15T00:00:00Z","annotations":{"hami.io/vgpu-mig-allocations":"[{\"gpuUUID\":\"GPU-worker-a-0\",\"profile\":\"1g.10gb\",\"placement\":{\"start\":0,\"size\":1}}]"}},"spec":{"nodeName":"worker-a"}},{"metadata":{"name":"new","namespace":"default","annotations":{"hami.io/vgpu-mig-allocations":"[{\"gpuUUID\":\"GPU-worker-a-0\",\"profile\":\"1g.10gb\",\"placement\":{\"start\":0,\"size\":1}}]"}},"spec":{"nodeName":"worker-a"}}]}`}
	a := &Agent{Node: "worker-a", State: H100State(), Kubectl: api}
	got, err := a.observeHAMi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "default/new/0" {
		t.Fatalf("terminating allocation was retained: %+v", got)
	}
}

func TestClearMutexOnlyForValidatedOwner(t *testing.T) {
	api := &mutexAPI{node: `{"metadata":{"annotations":{"hami.io/mutex.lock":"2026-09-14T00:00:00Z,team-a,job-b"}}}`}
	a := &Agent{Node: "worker-a", State: H100State(), Kubectl: api}
	a.State.Active["team-a/job-a/0"] = Decision{Key: "team-a/job-a/0"}
	if err := a.clearHAMiMutex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 1 {
		t.Fatalf("unmatched lock caused %d calls, want only GET", len(api.calls))
	}

	api.node = `{"metadata":{"annotations":{"hami.io/mutex.lock":"2026-09-14T00:00:00Z,team-a,job-a"}}}`
	if err := a.clearHAMiMutex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 3 || api.calls[2][0] != "annotate" {
		t.Fatalf("matched lock was not cleared: %#v", api.calls)
	}
}
