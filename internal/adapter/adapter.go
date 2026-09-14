package adapter

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

type Candidate struct {
	Node                            string
	GPU, Start, FreeGPC, FreeMemory int
}

type Adapter interface {
	Name() string
	Choose(model.Job, model.Profile, []Candidate) (Candidate, bool)
	Objects(model.Job) ([]byte, error)
}

type builtin struct{ cfg config.Backend }

func New(c config.Backend) Adapter { return &builtin{cfg: c} }
func (b *builtin) Name() string    { return b.cfg.Name }

func (b *builtin) Choose(_ model.Job, _ model.Profile, cs []Candidate) (Candidate, bool) {
	if len(cs) == 0 {
		return Candidate{}, false
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, bv := cs[i], cs[j]
		switch b.cfg.Policy {
		case "spread":
			return a.FreeGPC > bv.FreeGPC || (a.FreeGPC == bv.FreeGPC && less(a, bv))
		case "first-fit":
			return less(a, bv)
		default:
			return a.FreeGPC < bv.FreeGPC || (a.FreeGPC == bv.FreeGPC && less(a, bv))
		}
	})
	return cs[0], true
}

func less(a, b Candidate) bool {
	if a.Node != b.Node {
		return a.Node < b.Node
	}
	if a.GPU != b.GPU {
		return a.GPU < b.GPU
	}
	return a.Start < b.Start
}

func (b *builtin) Objects(j model.Job) ([]byte, error) {
	var obj any
	switch b.cfg.Kind {
	case "hami", "baseline":
		obj = map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": j.ID, "annotations": map[string]string{"hami.io/vgpu-mode": "mig", "migbench.io/profile": j.Profile}}, "spec": map[string]any{"schedulerName": "hami-scheduler", "containers": []any{map[string]any{"name": "workload", "image": "registry.k8s.io/pause:3.10.1", "resources": map[string]any{"limits": map[string]any{"nvidia.com/gpu": 1}}}}}}
	case "nvidia-dra":
		obj = []any{
			map[string]any{"apiVersion": "resource.k8s.io/v1", "kind": "ResourceClaim", "metadata": map[string]any{"name": j.ID}, "spec": map[string]any{"devices": map[string]any{"requests": []any{map[string]any{"name": "gpu", "exactly": map[string]any{"deviceClassName": "mig.nvidia.com", "selectors": []any{map[string]any{"cel": map[string]string{"expression": fmt.Sprintf("device.attributes['gpu.nvidia.com'].profile == %q", j.Profile)}}}}}}}}},
			map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": j.ID}, "spec": map[string]any{"resourceClaims": []any{map[string]any{"name": "gpu", "resourceClaimName": j.ID}}, "containers": []any{map[string]any{"name": "workload", "image": "registry.k8s.io/pause:3.10", "resources": map[string]any{"claims": []any{map[string]string{"name": "gpu"}}}}}}},
		}
	default:
		return nil, fmt.Errorf("unsupported adapter kind %q", b.cfg.Kind)
	}
	return json.MarshalIndent(obj, "", "  ")
}
