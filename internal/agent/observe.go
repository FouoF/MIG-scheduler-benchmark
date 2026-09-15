package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type objectList struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			Namespace         string            `json:"namespace"`
			Annotations       map[string]string `json:"annotations"`
			DeletionTimestamp string            `json:"deletionTimestamp"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Allocation *struct {
				Devices struct {
					Results []struct {
						Driver, Pool, Device, Request string
					}
				}
			}
		} `json:"status"`
	} `json:"items"`
}

type hamiAllocation struct {
	GPUUUID   string `json:"gpuUUID"`
	Profile   string `json:"profile"`
	Placement struct {
		Start int `json:"start"`
		Size  int `json:"size"`
	} `json:"placement"`
}

func (a *Agent) observeHAMi(ctx context.Context) ([]Decision, error) {
	b, err := a.Kubectl.Run(ctx, nil, "get", "pods", "--all-namespaces", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list objectList
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	var out []Decision
	for _, pod := range list.Items {
		if pod.Metadata.DeletionTimestamp != "" {
			continue
		}
		raw := pod.Metadata.Annotations["hami.io/vgpu-mig-allocations"]
		if raw == "" || pod.Spec.NodeName != a.Node {
			continue
		}
		var allocations []hamiAllocation
		if err := json.Unmarshal([]byte(raw), &allocations); err != nil {
			return nil, fmt.Errorf("pod %s/%s has malformed MIG allocation: %w", pod.Metadata.Namespace, pod.Metadata.Name, err)
		}
		for i, x := range allocations {
			out = append(out, Decision{Key: fmt.Sprintf("%s/%s/%d", pod.Metadata.Namespace, pod.Metadata.Name, i), Backend: "hami", Node: a.Node, GPU: x.GPUUUID, Profile: x.Profile, Start: x.Placement.Start, Size: x.Placement.Size})
		}
	}
	return out, nil
}

var draDevice = regexp.MustCompile(`^gpu([0-9]+)-mig-(1g10gb|2g20gb|3g40gb|7g80gb)-([0-6])$`)

func (a *Agent) observeDRA(ctx context.Context) ([]Decision, error) {
	b, err := a.Kubectl.Run(ctx, nil, "get", "resourceclaims", "--all-namespaces", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list objectList
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	profiles := map[string]string{"1g10gb": "1g.10gb", "2g20gb": "2g.20gb", "3g40gb": "3g.40gb", "7g80gb": "7g.80gb"}
	var out []Decision
	for _, claim := range list.Items {
		if claim.Metadata.DeletionTimestamp != "" {
			continue
		}
		if claim.Status.Allocation == nil {
			continue
		}
		for i, x := range claim.Status.Allocation.Devices.Results {
			if x.Driver != "gpu.nvidia.com" || x.Pool != a.Node {
				continue
			}
			m := draDevice.FindStringSubmatch(x.Device)
			if m == nil {
				return nil, fmt.Errorf("claim %s/%s allocated unknown device %q", claim.Metadata.Namespace, claim.Metadata.Name, x.Device)
			}
			gpu, _ := strconv.Atoi(m[1])
			start, _ := strconv.Atoi(m[3])
			profile := profiles[m[2]]
			p := a.State.Profiles[profile]
			out = append(out, Decision{Key: fmt.Sprintf("%s/%s/%d", claim.Metadata.Namespace, claim.Metadata.Name, i), Backend: "nvidia-dra", Node: a.Node, GPU: "GPU-" + a.Node + fmt.Sprintf("-%d", gpu), Profile: profile, Start: start, Size: p.Size})
		}
	}
	return out, nil
}

func (a *Agent) clearHAMiMutex(ctx context.Context) error {
	b, err := a.Kubectl.Run(ctx, nil, "get", "node", a.Node, "-o", "json")
	if err != nil {
		return err
	}
	var node struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(b, &node); err != nil {
		return err
	}
	found := false
	for key, value := range node.Metadata.Annotations {
		if key == "hami.io/mutex.lock" || strings.HasPrefix(key, "hami.io/mutex.lock/") {
			parts := strings.Split(value, ",")
			if len(parts) < 3 || !a.hasAllocation(strings.TrimSpace(parts[len(parts)-2]), strings.TrimSpace(parts[len(parts)-1])) {
				continue
			}
			found = true
			if _, err := a.Kubectl.Run(ctx, nil, "annotate", "node", a.Node, key+"-"); err != nil {
				return err
			}
		}
	}
	if found {
		a.emit("prepared", "", nil, "validated allocation and cleared HAMi node mutex")
	}
	return nil
}

func (a *Agent) hasAllocation(namespace, pod string) bool {
	prefix := namespace + "/" + pod + "/"
	for key := range a.State.Active {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
