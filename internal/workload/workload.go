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
	r := rand.New(rand.NewPCG(uint64(c.Workload.Seed), uint64(c.Workload.Seed)^0x9e3779b97f4a7c15))
	profiles, weights, err := weightedProfiles(c)
	if err != nil {
		return nil, err
	}
	jobs := make([]model.Job, 0, c.Workload.Jobs)
	var arrival int64
	for i := 0; i < c.Workload.Jobs; i++ {
		switch c.Workload.Model {
		case "poisson", "profile-skew":
			arrival += int64(r.ExpFloat64() * c.Workload.ArrivalMeanMS)
		case "burst":
			bs := c.Workload.BurstSize
			if bs < 1 {
				bs = 10
			}
			if i > 0 && i%bs == 0 {
				arrival += int64(c.Workload.ArrivalMeanMS)
			}
		case "adversarial":
			arrival += int64(c.Workload.ArrivalMeanMS)
		default:
			return nil, fmt.Errorf("unknown workload model %q", c.Workload.Model)
		}
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
		jobs = append(jobs, model.Job{ID: fmt.Sprintf("job-%06d", i+1), ArrivalMS: arrival, DurationMS: d, Profile: p})
	}
	return jobs, nil
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
