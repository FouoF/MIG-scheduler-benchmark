package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Agent struct {
	Backend string
	Node    string
	GPUs    int
	Kubectl API
	State   *State
	OnEvent func(Event)
}

type Event struct {
	Time     string    `json:"time"`
	Type     string    `json:"type"`
	Backend  string    `json:"backend"`
	Key      string    `json:"key,omitempty"`
	Message  string    `json:"message"`
	Decision *Decision `json:"decision,omitempty"`
}

func New(backend, node string, kubectl API) (*Agent, error) {
	if backend != "hami" && backend != "nvidia-dra" {
		return nil, fmt.Errorf("unsupported backend %q", backend)
	}
	return &Agent{Backend: backend, Node: node, GPUs: 1, Kubectl: kubectl, State: H100State()}, nil
}

func (a *Agent) Publish(ctx context.Context) error {
	var err error
	if a.Backend == "hami" {
		err = a.publishHAMi(ctx)
	} else {
		err = a.publishDRA(ctx)
	}
	if err == nil {
		a.emit("published", "", nil, "simulated H100 inventory published")
	}
	return err
}

func (a *Agent) Reconcile(ctx context.Context) error {
	var decisions []Decision
	var err error
	if a.Backend == "hami" {
		decisions, err = a.observeHAMi(ctx)
	} else {
		decisions, err = a.observeDRA(ctx)
	}
	if err != nil {
		return err
	}
	previous := a.State.Active
	if err := a.State.Replace(decisions); err != nil {
		a.emit("invalid", "", nil, err.Error())
		return err
	}
	added := false
	for key, d := range a.State.Active {
		if _, ok := previous[key]; !ok {
			added = true
			d := d
			a.emit("allocated", key, &d, "scheduler decision accepted")
		}
	}
	for key, d := range previous {
		if _, ok := a.State.Active[key]; !ok {
			d := d
			a.emit("released", key, &d, "allocation object disappeared")
		}
	}
	if a.Backend == "hami" && added {
		return a.clearHAMiMutex(ctx)
	}
	return nil
}

func (a *Agent) Run(ctx context.Context, interval time.Duration) error {
	if err := a.Publish(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.Reconcile(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *Agent) emit(kind, key string, d *Decision, message string) {
	if a.OnEvent != nil {
		a.OnEvent(Event{Time: time.Now().UTC().Format(time.RFC3339Nano), Type: kind, Backend: a.Backend, Key: key, Decision: d, Message: message})
	}
}

func (a *Agent) publishHAMi(ctx context.Context) error {
	if a.GPUs < 1 {
		return fmt.Errorf("GPU count must be positive")
	}
	status := map[string]any{"status": map[string]any{
		"capacity":    map[string]string{"nvidia.com/gpu": fmt.Sprint(7 * a.GPUs), "nvidia.com/gpumem": fmt.Sprint(81920 * a.GPUs), "nvidia.com/gpucores": fmt.Sprint(100 * a.GPUs)},
		"allocatable": map[string]string{"nvidia.com/gpu": fmt.Sprint(7 * a.GPUs), "nvidia.com/gpumem": fmt.Sprint(81920 * a.GPUs), "nvidia.com/gpucores": fmt.Sprint(100 * a.GPUs)},
	}}
	b, _ := json.Marshal(status)
	if _, err := a.Kubectl.Run(ctx, nil, "patch", "node", a.Node, "--subresource=status", "--type=merge", "-p", string(b)); err != nil {
		return err
	}
	inventory := make([]any, 0, a.GPUs)
	for gpu := 0; gpu < a.GPUs; gpu++ {
		inventory = append(inventory, map[string]any{
			"id": "GPU-" + a.Node + fmt.Sprintf("-%d", gpu), "index": gpu, "count": 7,
			"devmem": 81920, "devcore": 100, "type": "NVIDIA-H100-SXM5-80GB",
			"health": true, "numa": 0, "mode": "mig",
			"migProfiles": []any{
				migProfile("1g.10gb", 10240, 14, 1, 0, 1, 2, 3, 4, 5, 6),
				migProfile("2g.20gb", 20480, 28, 2, 0, 2, 4),
				migProfile("3g.40gb", 40960, 42, 3, 0, 4),
				migProfile("7g.80gb", 81920, 100, 7, 0),
			},
		})
	}
	reg, _ := json.Marshal(inventory)
	_, err := a.Kubectl.Run(ctx, nil, "annotate", "node", a.Node, "hami.io/node-nvidia-register="+string(reg), "--overwrite")
	return err
}

func migProfile(name string, memory, compute, size int, starts ...int) map[string]any {
	placements := make([]map[string]int, 0, len(starts))
	for _, start := range starts {
		placements = append(placements, map[string]int{"start": start, "size": size})
	}
	return map[string]any{"name": name, "memoryMB": memory, "core": compute, "sliceCount": size, "placements": placements}
}

func (a *Agent) publishDRA(ctx context.Context) error {
	if a.GPUs < 1 {
		return fmt.Errorf("GPU count must be positive")
	}
	class := map[string]any{"apiVersion": "resource.k8s.io/v1", "kind": "DeviceClass", "metadata": map[string]any{"name": "mig.nvidia.com"}, "spec": map[string]any{"selectors": []any{map[string]any{"cel": map[string]string{"expression": "device.driver == 'gpu.nvidia.com' && device.attributes['gpu.nvidia.com'].type == 'mig'"}}}}}
	if err := a.Kubectl.Apply(ctx, class); err != nil {
		return err
	}
	sharedCounters := make([]any, 0, a.GPUs)
	for gpu := 0; gpu < a.GPUs; gpu++ {
		counters := map[string]any{}
		for i := 0; i < a.State.Slots; i++ {
			counters[fmt.Sprintf("memory-slice-%d", i)] = map[string]string{"value": "1"}
		}
		sharedCounters = append(sharedCounters, map[string]any{"name": fmt.Sprintf("gpu-%d-counter-set", gpu), "counters": counters})
	}
	pool := map[string]any{"name": a.Node, "generation": 1, "resourceSliceCount": 2}
	counterSlice := map[string]any{"apiVersion": "resource.k8s.io/v1", "kind": "ResourceSlice", "metadata": map[string]any{"name": safeName(a.Node) + "-h100-counters"}, "spec": map[string]any{"driver": "gpu.nvidia.com", "nodeName": a.Node, "pool": pool, "sharedCounters": sharedCounters}}
	if err := a.Kubectl.Apply(ctx, counterSlice); err != nil {
		return err
	}
	var devices []any
	names := make([]string, 0, len(a.State.Profiles))
	for name := range a.State.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for gpu := 0; gpu < a.GPUs; gpu++ {
		for _, name := range names {
			p := a.State.Profiles[name]
			starts := make([]int, 0, len(p.LegalStarts))
			for start := range p.LegalStarts {
				starts = append(starts, start)
			}
			sort.Ints(starts)
			for _, start := range starts {
				consume := map[string]any{}
				for slot := start; slot < start+p.Size; slot++ {
					consume[fmt.Sprintf("memory-slice-%d", slot)] = map[string]string{"value": "1"}
				}
				devices = append(devices, map[string]any{
					"name":             deviceName(gpu, p.Name, start),
					"attributes":       map[string]any{"gpu.nvidia.com/type": map[string]string{"string": "mig"}, "gpu.nvidia.com/profile": map[string]string{"string": p.Name}, "gpu.nvidia.com/index": map[string]any{"int": gpu}},
					"capacity":         map[string]any{"gpu.nvidia.com/memory": map[string]string{"value": fmt.Sprintf("%dMi", p.MemoryMB)}, "gpu.nvidia.com/multiprocessors": map[string]string{"value": fmt.Sprint(p.Compute)}},
					"consumesCounters": []any{map[string]any{"counterSet": fmt.Sprintf("gpu-%d-counter-set", gpu), "counters": consume}},
				})
			}
		}
	}
	deviceSlice := map[string]any{"apiVersion": "resource.k8s.io/v1", "kind": "ResourceSlice", "metadata": map[string]any{"name": safeName(a.Node) + "-h100-devices"}, "spec": map[string]any{"driver": "gpu.nvidia.com", "nodeName": a.Node, "pool": pool, "devices": devices}}
	return a.Kubectl.Apply(ctx, deviceSlice)
}

func deviceName(gpu int, profile string, start int) string {
	return fmt.Sprintf("gpu%d-mig-%s-%d", gpu, strings.ReplaceAll(profile, ".", ""), start)
}

func safeName(s string) string {
	s = strings.ToLower(s)
	return regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(s, "-")
}
