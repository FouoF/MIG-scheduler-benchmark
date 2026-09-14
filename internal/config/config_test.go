package config

import "testing"

func TestControlPlaneFailsHonestly(t *testing.T) {
	c := Config{Cluster: Cluster{Nodes: 1, GPUsPerNode: 1, Model: "H100-80GB", GPCPerGPU: 7, MemoryGB: 80, Profiles: H100Profiles()}, Backends: []Backend{{Name: "real", Kind: "hami", Mode: "control-plane"}}}
	if c.Validate() == nil {
		t.Fatal("control-plane mode must fail until bridge exists")
	}
}
