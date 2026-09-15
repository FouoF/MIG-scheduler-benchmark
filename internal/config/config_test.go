package config

import "testing"

func TestControlPlaneRequiresExplicitNodes(t *testing.T) {
	c := Config{Cluster: Cluster{Nodes: 1, GPUsPerNode: 1, Model: "H100-80GB", GPCPerGPU: 7, MemoryGB: 80, Profiles: H100Profiles()}, Backends: []Backend{{Name: "real", Kind: "hami", Mode: "control-plane", GitRef: "abc", Image: "image@sha256:abc", ManifestChecksum: "abc"}}}
	if c.Validate() == nil {
		t.Fatal("control-plane mode must require an explicit node set")
	}
	c.Backends[0].Parameters = map[string]string{"nodes": "worker-a"}
	if err := c.Validate(); err != nil {
		t.Fatalf("configured control-plane backend rejected: %v", err)
	}
}
