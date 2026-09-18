package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"sort"

	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

func Generate(c config.Config) ([]model.Job, error) {
	format := c.Workload.Format
	if format == "" {
		format = "resource-bound-v2"
	}
	r := rand.New(rand.NewPCG(uint64(c.Workload.Seed), uint64(c.Workload.Seed)^0x9e3779b97f4a7c15))
	profiles, weights, err := weightedProfiles(c)
	if err != nil {
		return nil, err
	}
	jobs := make([]model.Job, 0, c.Workload.Jobs)
	for i := 0; i < c.Workload.Jobs; i++ {
		p := choose(r, profiles, weights)
		if c.Workload.Model == "adversarial" {
			if i < c.Workload.Jobs/2 {
				p = profiles[0]
			} else {
				p = profiles[len(profiles)-1]
			}
		}
		d := int64(math.Exp(math.Log(c.Workload.DurationMedianMS) + c.Workload.DurationSigma*r.NormFloat64()))
		if d < 1 {
			d = 1
		}
		if c.Workload.DurationMaxMS > 0 && d > c.Workload.DurationMaxMS {
			d = c.Workload.DurationMaxMS
		}
		job := model.Job{ID: fmt.Sprintf("job-%06d", i+1), Format: format}
		if format == "profile-bound-v1" {
			job.DurationMS, job.Profile = d, p
		} else {
			profile := profileByName(c.Cluster.Profiles, p)
			job.MemoryMB = profile.MemoryGB * 1024
			job.ComputeCoreMS = d * int64(profile.ComputePercent)
		}
		jobs = append(jobs, job)
	}
	arrivalMean, err := arrivalMeanMS(c, jobs)
	if err != nil {
		return nil, err
	}
	// Keep arrival randomness independent from profile and duration draws. This
	// makes load-control changes leave the resource sequence untouched.
	ar := rand.New(rand.NewPCG(uint64(c.Workload.Seed)^0xd1b54a32d192ed03, uint64(c.Workload.Seed)^0x94d049bb133111eb))
	var arrival int64
	for i := range jobs {
		switch c.Workload.Model {
		case "poisson", "profile-skew":
			arrival += int64(ar.ExpFloat64() * arrivalMean)
		case "burst":
			bs := c.Workload.BurstSize
			if bs < 1 {
				bs = 10
			}
			if i > 0 && i%bs == 0 {
				arrival += int64(arrivalMean * float64(bs))
			}
		case "adversarial":
			arrival += int64(arrivalMean)
		default:
			return nil, fmt.Errorf("unknown workload model %q", c.Workload.Model)
		}
		jobs[i].ArrivalMS = arrival
	}
	return jobs, nil
}

func arrivalMeanMS(c config.Config, jobs []model.Job) (float64, error) {
	if c.Workload.ArrivalMeanMS > 0 {
		return c.Workload.ArrivalMeanMS, nil
	}
	if c.Workload.TargetOfferedLoad <= 0 {
		return 0, fmt.Errorf("targetOfferedLoad or arrivalMeanMS must be positive")
	}
	totalGPC := c.Cluster.Nodes * c.Cluster.GPUsPerNode * c.Cluster.GPCPerGPU
	if totalGPC <= 0 || len(jobs) == 0 {
		return 0, fmt.Errorf("cannot derive arrival rate without jobs and cluster capacity")
	}
	var occupiedGPCMS float64
	for _, j := range jobs {
		if j.Profile != "" {
			p := profileByName(c.Cluster.Profiles, j.Profile)
			occupiedGPCMS += float64(p.GPC) * float64(j.DurationMS)
			continue
		}
		p, ok := smallestProfileForMemory(c.Cluster.Profiles, j.MemoryMB)
		if !ok {
			return 0, fmt.Errorf("job %s memory request has no feasible profile", j.ID)
		}
		runtime := (j.ComputeCoreMS + int64(p.ComputePercent) - 1) / int64(p.ComputePercent)
		occupiedGPCMS += float64(p.GPC) * float64(runtime)
	}
	return occupiedGPCMS / float64(len(jobs)*totalGPC) / c.Workload.TargetOfferedLoad, nil
}

func smallestProfileForMemory(profiles []model.Profile, memoryMB int) (model.Profile, bool) {
	ps := append([]model.Profile(nil), profiles...)
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].GPC < ps[j].GPC || (ps[i].GPC == ps[j].GPC && ps[i].MemoryGB < ps[j].MemoryGB)
	})
	for _, p := range ps {
		if p.MemoryGB*1024 >= memoryMB {
			return p, true
		}
	}
	return model.Profile{}, false
}

func profileByName(profiles []model.Profile, name string) model.Profile {
	for _, p := range profiles {
		if p.Name == name {
			return p
		}
	}
	panic("validated profile missing")
}

func weightedProfiles(c config.Config) ([]string, []float64, error) {
	ps := append([]model.Profile(nil), c.Cluster.Profiles...)
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].GPC < ps[j].GPC || (ps[i].GPC == ps[j].GPC && ps[i].MemoryGB < ps[j].MemoryGB)
	})
	names := make([]string, len(ps))
	weights := make([]float64, len(ps))
	for i, p := range ps {
		names[i] = p.Name
		weights[i] = c.Workload.ProfileWeights[p.Name]
		if len(c.Workload.ProfileWeights) == 0 {
			weights[i] = 1
		}
	}
	var total float64
	for _, w := range weights {
		if w < 0 {
			return nil, nil, fmt.Errorf("negative profile weight")
		}
		total += w
	}
	if total == 0 {
		return nil, nil, fmt.Errorf("profile weights sum to zero")
	}
	return names, weights, nil
}

func choose(r *rand.Rand, names []string, weights []float64) string {
	var total float64
	for _, w := range weights {
		total += w
	}
	x := r.Float64() * total
	for i, w := range weights {
		x -= w
		if x <= 0 {
			return names[i]
		}
	}
	return names[len(names)-1]
}

func Write(path string, jobs []model.Job) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	e := json.NewEncoder(f)
	for _, j := range jobs {
		if err := e.Encode(j); err != nil {
			return err
		}
	}
	return nil
}

func Read(path string) ([]model.Job, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var jobs []model.Job
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1024), 1024*1024)
	for s.Scan() {
		var j model.Job
		if err := json.Unmarshal(s.Bytes(), &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ArrivalMS < jobs[j].ArrivalMS })
	return jobs, nil
}
