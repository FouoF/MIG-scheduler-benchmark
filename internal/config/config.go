package config

import (
	"fmt"
	"os"

	"github.com/dynamia-ai/migbench/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Name     string    `yaml:"name" json:"name"`
	Cluster  Cluster   `yaml:"cluster" json:"cluster"`
	Backends []Backend `yaml:"backends" json:"backends"`
	Workload Workload  `yaml:"workload" json:"workload"`
	Costs    Costs     `yaml:"costs" json:"costs"`
	Limits   Limits    `yaml:"limits" json:"limits"`
}

type Cluster struct {
	Nodes       int             `yaml:"nodes" json:"nodes"`
	GPUsPerNode int             `yaml:"gpusPerNode" json:"gpusPerNode"`
	Model       string          `yaml:"model" json:"model"`
	GPCPerGPU   int             `yaml:"gpcPerGPU" json:"gpcPerGPU"`
	MemoryGB    int             `yaml:"memoryGB" json:"memoryGB"`
	Profiles    []model.Profile `yaml:"profiles" json:"profiles"`
}

type Backend struct {
	Name             string            `yaml:"name" json:"name"`
	Kind             string            `yaml:"kind" json:"kind"`
	Mode             string            `yaml:"mode" json:"mode"`
	Policy           string            `yaml:"policy" json:"policy"`
	GitRef           string            `yaml:"gitRef" json:"gitRef"`
	Image            string            `yaml:"image" json:"image"`
	ManifestChecksum string            `yaml:"manifestChecksum" json:"manifestChecksum"`
	Parameters       map[string]string `yaml:"parameters" json:"parameters"`
}

type Workload struct {
	Format string `yaml:"format" json:"format"`
	// PrefillJobs is retained only so obsolete configurations fail loudly.
	// Synthetic prefill changes both arrivals and placement geometry and must
	// not be used by a scheduling benchmark.
	PrefillJobs       int                `yaml:"prefillJobs,omitempty" json:"prefillJobs,omitempty"`
	Seed              int64              `yaml:"seed" json:"seed"`
	Jobs              int                `yaml:"jobs" json:"jobs"`
	Model             string             `yaml:"model" json:"model"`
	ArrivalMeanMS     float64            `yaml:"arrivalMeanMS" json:"arrivalMeanMS"`
	TargetOfferedLoad float64            `yaml:"targetOfferedLoad" json:"targetOfferedLoad"`
	DurationMedianMS  float64            `yaml:"durationMedianMS" json:"durationMedianMS"`
	DurationMaxMS     int64              `yaml:"durationMaxMS" json:"durationMaxMS"`
	DurationSigma     float64            `yaml:"durationSigma" json:"durationSigma"`
	BurstSize         int                `yaml:"burstSize" json:"burstSize"`
	ProfileWeights    map[string]float64 `yaml:"profileWeights" json:"profileWeights"`
	Repetitions       int                `yaml:"repetitions" json:"repetitions"`
}

type Costs struct {
	CreateMS      int64 `yaml:"createMS" json:"createMS"`
	DeleteMS      int64 `yaml:"deleteMS" json:"deleteMS"`
	ReconfigureMS int64 `yaml:"reconfigureMS" json:"reconfigureMS"`
}

type Limits struct {
	SchedulingTimeoutMS      int64   `yaml:"schedulingTimeoutMS" json:"schedulingTimeoutMS"`
	SampleEveryMS            int64   `yaml:"sampleEveryMS" json:"sampleEveryMS"`
	MeasurementStartMS       int64   `yaml:"measurementStartMS" json:"measurementStartMS"`
	MinAverageGPCUtilization float64 `yaml:"minAverageGPCUtilization" json:"minAverageGPCUtilization"`
	MinBacklogFraction       float64 `yaml:"minBacklogFraction" json:"minBacklogFraction"`
	RequireHighLoad          bool    `yaml:"requireHighLoad" json:"requireHighLoad"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	c.defaults()
	return c, c.Validate()
}

func (c *Config) defaults() {
	if c.Cluster.Model == "" {
		c.Cluster.Model = "H100-80GB"
	}
	if c.Cluster.Nodes == 0 {
		c.Cluster.Nodes = 2
	}
	if c.Cluster.GPUsPerNode == 0 {
		c.Cluster.GPUsPerNode = 4
	}
	if c.Cluster.GPCPerGPU == 0 {
		c.Cluster.GPCPerGPU = 7
	}
	if c.Cluster.MemoryGB == 0 {
		c.Cluster.MemoryGB = 80
	}
	if len(c.Cluster.Profiles) == 0 {
		c.Cluster.Profiles = H100Profiles()
	}
	if c.Workload.Jobs == 0 {
		c.Workload.Jobs = 100
	}
	if c.Workload.ArrivalMeanMS == 0 && c.Workload.TargetOfferedLoad == 0 {
		c.Workload.TargetOfferedLoad = 1.10
	}
	if c.Workload.DurationMedianMS == 0 {
		c.Workload.DurationMedianMS = 60000
	}
	if c.Workload.DurationMaxMS == 0 {
		c.Workload.DurationMaxMS = 3600000
	}
	if c.Workload.DurationSigma == 0 {
		c.Workload.DurationSigma = .8
	}
	if c.Workload.Repetitions == 0 {
		c.Workload.Repetitions = 1
	}
	if c.Workload.Model == "" {
		c.Workload.Model = "poisson"
	}
	if c.Workload.Format == "" {
		c.Workload.Format = "resource-bound-v2"
	}
	if c.Costs.CreateMS == 0 {
		c.Costs.CreateMS = 250
	}
	if c.Costs.DeleteMS == 0 {
		c.Costs.DeleteMS = 100
	}
	if c.Costs.ReconfigureMS == 0 {
		c.Costs.ReconfigureMS = 1500
	}
	if c.Limits.SchedulingTimeoutMS == 0 {
		c.Limits.SchedulingTimeoutMS = 30000
	}
	if c.Limits.SampleEveryMS == 0 {
		c.Limits.SampleEveryMS = 1000
	}
	if c.Limits.MinAverageGPCUtilization == 0 {
		c.Limits.MinAverageGPCUtilization = .75
	}
	if c.Limits.MinBacklogFraction == 0 {
		c.Limits.MinBacklogFraction = .90
	}
	for i := range c.Backends {
		if c.Backends[i].Mode == "" {
			c.Backends[i].Mode = "simulated"
		}
		if c.Backends[i].Policy == "" {
			c.Backends[i].Policy = "binpack"
		}
	}
}

func (c Config) Validate() error {
	if c.Cluster.Model != "H100-80GB" {
		return fmt.Errorf("v1 supports cluster.model H100-80GB, got %q", c.Cluster.Model)
	}
	if c.Cluster.Nodes < 1 || c.Cluster.GPUsPerNode < 1 {
		return fmt.Errorf("cluster nodes and GPUsPerNode must be positive")
	}
	if c.Workload.PrefillJobs != 0 {
		return fmt.Errorf("workload.prefillJobs is obsolete: start from an empty cluster and use targetOfferedLoad")
	}
	if c.Workload.ArrivalMeanMS > 0 && c.Workload.TargetOfferedLoad > 0 {
		return fmt.Errorf("set only one of workload.arrivalMeanMS and workload.targetOfferedLoad")
	}
	if c.Workload.ArrivalMeanMS < 0 || c.Workload.TargetOfferedLoad < 0 {
		return fmt.Errorf("arrivalMeanMS and targetOfferedLoad cannot be negative")
	}
	if c.Workload.TargetOfferedLoad > 0 && c.Workload.TargetOfferedLoad < .01 {
		return fmt.Errorf("targetOfferedLoad must be at least 0.01")
	}
	if c.Workload.DurationMaxMS < 0 {
		return fmt.Errorf("durationMaxMS cannot be negative")
	}
	if c.Limits.MinAverageGPCUtilization < 0 || c.Limits.MinAverageGPCUtilization > 1 {
		return fmt.Errorf("minAverageGPCUtilization must be in [0,1]")
	}
	if len(c.Backends) == 0 {
		return fmt.Errorf("at least one backend is required")
	}
	seen := map[string]bool{}
	for _, p := range c.Cluster.Profiles {
		if p.Name == "" || p.GPC < 1 || p.GPC > c.Cluster.GPCPerGPU || p.MemoryGB < 1 || p.MemoryGB > c.Cluster.MemoryGB {
			return fmt.Errorf("invalid profile %+v", p)
		}
		seen[p.Name] = true
		if p.ComputePercent < 1 || p.ComputePercent > 100 {
			return fmt.Errorf("profile %s has invalid computePercent %d", p.Name, p.ComputePercent)
		}
	}
	for _, b := range c.Backends {
		if b.Name == "" || (b.Kind != "hami" && b.Kind != "nvidia-dra" && b.Kind != "baseline") {
			return fmt.Errorf("invalid backend %+v", b)
		}
		if b.Kind != "baseline" && (b.GitRef == "" || b.Image == "" || b.ManifestChecksum == "") {
			return fmt.Errorf("backend %s must lock gitRef, image digest, and manifestChecksum", b.Name)
		}
		if b.Mode != "simulated" && b.Mode != "control-plane" {
			return fmt.Errorf("backend %s: invalid mode %q", b.Name, b.Mode)
		}
		if b.Mode == "control-plane" {
			if b.Kind == "baseline" {
				return fmt.Errorf("backend %s: baseline does not have a Kubernetes control-plane mode", b.Name)
			}
			if b.Parameters["nodes"] == "" {
				return fmt.Errorf("backend %s: control-plane mode requires parameters.nodes", b.Name)
			}
		}
	}
	for p := range c.Workload.ProfileWeights {
		if !seen[p] {
			return fmt.Errorf("workload references unknown profile %q", p)
		}
	}
	return nil
}

func H100Profiles() []model.Profile {
	return []model.Profile{
		{Name: "1g.10gb", GPC: 1, MemoryGB: 10, ComputePercent: 14, Placements: []int{0, 1, 2, 3, 4, 5, 6}},
		{Name: "1g.20gb", GPC: 1, MemoryGB: 20, ComputePercent: 14, Placements: []int{0, 1, 2, 3, 4, 5, 6}},
		{Name: "2g.20gb", GPC: 2, MemoryGB: 20, ComputePercent: 28, Placements: []int{0, 2, 4}},
		{Name: "3g.40gb", GPC: 3, MemoryGB: 40, ComputePercent: 42, Placements: []int{0, 4}},
		{Name: "4g.40gb", GPC: 4, MemoryGB: 40, ComputePercent: 57, Placements: []int{0}},
		{Name: "7g.80gb", GPC: 7, MemoryGB: 80, ComputePercent: 100, Placements: []int{0}},
	}
}
