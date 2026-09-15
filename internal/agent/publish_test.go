package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeAPI struct {
	applied []map[string]any
	calls   [][]string
}

func (f *fakeAPI) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if len(args) >= 2 && args[0] == "get" {
		return []byte(`{"items":[]}`), nil
	}
	return nil, nil
}

func (f *fakeAPI) Apply(_ context.Context, object any) error {
	b, _ := json.Marshal(object)
	var decoded map[string]any
	_ = json.Unmarshal(b, &decoded)
	f.applied = append(f.applied, decoded)
	return nil
}

func TestPublishDRAMultipleGPUs(t *testing.T) {
	fake := &fakeAPI{}
	a, err := New("nvidia-dra", "worker-a", fake)
	if err != nil {
		t.Fatal(err)
	}
	a.GPUs = 2
	if err := a.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.applied) != 3 {
		t.Fatalf("applied %d objects, want 3", len(fake.applied))
	}
	counterSpec := fake.applied[1]["spec"].(map[string]any)
	if got := len(counterSpec["sharedCounters"].([]any)); got != 2 {
		t.Fatalf("shared counter sets = %d, want 2", got)
	}
	deviceSpec := fake.applied[2]["spec"].(map[string]any)
	devices := deviceSpec["devices"].([]any)
	if got := len(devices); got != 26 {
		t.Fatalf("placement devices = %d, want 26", got)
	}
	if got := devices[13].(map[string]any)["name"]; got != "gpu1-mig-1g10gb-0" {
		t.Fatalf("first GPU 1 device = %v", got)
	}
}

func TestPublishHAMiMultipleGPUs(t *testing.T) {
	fake := &fakeAPI{}
	a, err := New("hami", "worker-a", fake)
	if err != nil {
		t.Fatal(err)
	}
	a.GPUs = 2
	if err := a.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("kubectl calls = %d, want 2", len(fake.calls))
	}
	patch := strings.Join(fake.calls[0], " ")
	if !strings.Contains(patch, `"nvidia.com/gpu":"14"`) {
		t.Fatalf("node capacity does not contain 14 MIG slots: %s", patch)
	}
	if !strings.Contains(patch, `"nvidia.com/gpucores":"200000"`) {
		t.Fatalf("node capacity does not contain the non-binding admission shim: %s", patch)
	}
	annotation := fake.calls[1][3]
	parts := strings.SplitN(annotation, "=", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed annotation argument %q", annotation)
	}
	var inventory []map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 2 || inventory[1]["id"] != "GPU-worker-a-1" {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
}
