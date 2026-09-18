package simulator

import (
	"fmt"
	"sort"
	"time"

	"github.com/dynamia-ai/migbench/internal/adapter"
	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

type allocation struct {
	job           model.Job
	profile       model.Profile
	start, finish int64
	node          string
	gpu, slot     int
}
type gpu struct {
	slots      []string
	memoryUsed int
}
type node struct {
	name   string
	labels map[string]string
	gpus   []gpu
}

type Result struct {
	Summary model.Summary     `json:"summary"`
	Jobs    []model.JobResult `json:"jobs"`
	Events  []model.Event     `json:"events"`
	Objects map[string]string `json:"objects"`
}

type engine struct {
	c                                        config.Config
	backend                                  config.Backend
	adapter                                  adapter.Adapter
	profiles                                 map[string]model.Profile
	nodes                                    []node
	pending                                  []model.Job
	running                                  map[string]allocation
	results                                  []model.JobResult
	events                                   []model.Event
	objects                                  map[string]string
	fragmentedJobs                           map[string]bool
	now, last                                int64
	gpcArea, memArea, fragArea, strandedArea float64
	creates, deletes, reconfigs, reqFrag     int
	lastArrival, measurementMS, backlogMS    int64
	peakGPCUtilization                       float64
	physicalGPCMS, strandedGPCMS             int64
	blockedGPCMS, blockedMemoryMBMS          int64
}

func Run(c config.Config, b config.Backend, jobs []model.Job) (Result, error) {
	seenJobs := map[string]bool{}
	for _, j := range jobs {
		if j.ID == "" || seenJobs[j.ID] {
			return Result{}, fmt.Errorf("job IDs must be non-empty and unique: %q", j.ID)
		}
		seenJobs[j.ID] = true
		if j.ArrivalMS < 0 || (j.Profile == "" && (j.ComputeCoreMS < 1 || j.MemoryMB < 1)) || (j.Profile != "" && j.DurationMS < 1) {
			return Result{}, fmt.Errorf("job %s has invalid timing", j.ID)
		}
	}
	e := &engine{c: c, backend: b, adapter: adapter.New(b), profiles: map[string]model.Profile{}, running: map[string]allocation{}, objects: map[string]string{}, fragmentedJobs: map[string]bool{}}
	for _, p := range c.Cluster.Profiles {
		e.profiles[p.Name] = p
	}
	for i := 0; i < c.Cluster.Nodes; i++ {
		n := node{name: fmt.Sprintf("mig-node-%03d", i+1), labels: map[string]string{"migbench.io/gpu-model": c.Cluster.Model}}
		for g := 0; g < c.Cluster.GPUsPerNode; g++ {
			n.gpus = append(n.gpus, gpu{slots: make([]string, c.Cluster.GPCPerGPU)})
		}
		e.nodes = append(e.nodes, n)
	}
	jobs = append([]model.Job(nil), jobs...)
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ArrivalMS < jobs[j].ArrivalMS })
	if len(jobs) > 0 {
		e.lastArrival = jobs[len(jobs)-1].ArrivalMS
	}
	idx := 0
	for idx < len(jobs) || len(e.pending) > 0 || len(e.running) > 0 {
		next := int64(^uint64(0) >> 1)
		if idx < len(jobs) && jobs[idx].ArrivalMS < next {
			next = jobs[idx].ArrivalMS
		}
		for _, a := range e.running {
			if a.finish < next {
				next = a.finish
			}
		}
		if next == int64(^uint64(0)>>1) {
			break
		}
		e.integrate(next - e.now)
		e.now = next
		for id, a := range e.running {
			if a.finish == e.now {
				e.release(a)
				delete(e.running, id)
			}
		}
		for idx < len(jobs) && jobs[idx].ArrivalMS == e.now {
			e.pending = append(e.pending, jobs[idx])
			e.emit("arrival", jobs[idx], "", -1, "")
			idx++
		}
		e.schedule()
		if len(e.running) == 0 && idx >= len(jobs) && len(e.pending) > 0 {
			return Result{}, fmt.Errorf("%d jobs can never be placed; first is %s (%s)", len(e.pending), e.pending[0].ID, e.pending[0].Profile)
		}
	}
	s := e.summary(len(jobs))
	return Result{Summary: s, Jobs: e.results, Events: e.events, Objects: e.objects}, nil
}

func (e *engine) schedule() {
	for {
		progress := false
		for i := 0; i < len(e.pending); {
			j := e.pending[i]
			p, ok := e.resolveProfile(j)
			if !ok {
				i++
				continue
			}
			cs, totalEnough := e.candidates(j, p)
			t := time.Now()
			choice, ok := e.adapter.Choose(j, p, cs)
			lat := time.Since(t).Microseconds()
			if !ok {
				if totalEnough && !e.fragmentedJobs[j.ID] {
					e.reqFrag++
					e.fragmentedJobs[j.ID] = true
				}
				i++
				continue
			}
			start := e.now + e.c.Costs.CreateMS
			runtime := j.DurationMS
			if j.ComputeCoreMS > 0 {
				runtime = (j.ComputeCoreMS + int64(p.ComputePercent) - 1) / int64(p.ComputePercent)
			}
			finish := start + runtime + e.c.Costs.DeleteMS
			g := &e.nodesByName(choice.Node).gpus[choice.GPU]
			for s := choice.Start; s < choice.Start+p.GPC; s++ {
				g.slots[s] = j.ID
			}
			g.memoryUsed += p.MemoryGB
			j.Profile = p.Name
			a := allocation{job: j, profile: p, start: start, finish: finish, node: choice.Node, gpu: choice.GPU, slot: choice.Start}
			e.running[j.ID] = a
			e.creates++
			e.results = append(e.results, model.JobResult{JobID: j.ID, Profile: p.Name, ArrivalMS: j.ArrivalMS, StartMS: start, FinishMS: finish, WaitMS: start - j.ArrivalMS, SchedulingUS: lat, Node: choice.Node, GPU: choice.GPU, RequestFragmented: e.fragmentedJobs[j.ID], ComputeCoreMS: j.ComputeCoreMS, RequestedMemoryMB: j.MemoryMB, AllocatedMemoryMB: p.MemoryGB * 1024, AllocatedCompute: p.ComputePercent, RuntimeMS: runtime})
			if raw, err := e.adapter.Objects(j); err == nil {
				e.objects[j.ID] = string(raw)
			}
			e.emit("scheduled", j, choice.Node, choice.GPU, fmt.Sprintf("slot=%d", choice.Start))
			e.pending = append(e.pending[:i], e.pending[i+1:]...)
			progress = true
		}
		if !progress {
			return
		}
	}
}

func (e *engine) resolveProfile(j model.Job) (model.Profile, bool) {
	if j.Profile != "" {
		p, ok := e.profiles[j.Profile]
		return p, ok
	}
	var feasible []model.Profile
	for _, p := range e.c.Cluster.Profiles {
		if p.MemoryGB*1024 >= j.MemoryMB {
			feasible = append(feasible, p)
		}
	}
	if len(feasible) == 0 {
		return model.Profile{}, false
	}
	sort.Slice(feasible, func(i, k int) bool {
		if feasible[i].GPC != feasible[k].GPC {
			return feasible[i].GPC < feasible[k].GPC
		}
		if feasible[i].MemoryGB != feasible[k].MemoryGB {
			return feasible[i].MemoryGB < feasible[k].MemoryGB
		}
		return feasible[i].Name < feasible[k].Name
	})
	return feasible[0], true
}

func (e *engine) candidates(j model.Job, p model.Profile) ([]adapter.Candidate, bool) {
	var out []adapter.Candidate
	totalFreeGPC, totalFreeMem := 0, 0
	for ni := range e.nodes {
		n := &e.nodes[ni]
		if !matches(j.NodeSelector, n.labels) {
			continue
		}
		for gi := range n.gpus {
			g := &n.gpus[gi]
			free := 0
			for _, x := range g.slots {
				if x == "" {
					free++
				}
			}
			fm := e.c.Cluster.MemoryGB - g.memoryUsed
			totalFreeGPC += free
			totalFreeMem += fm
			if fm < p.MemoryGB {
				continue
			}
			starts := p.Placements
			if len(starts) == 0 {
				for s := 0; s+p.GPC <= len(g.slots); s++ {
					starts = append(starts, s)
				}
			}
			for _, s := range starts {
				if s < 0 || s+p.GPC > len(g.slots) {
					continue
				}
				ok := true
				for x := s; x < s+p.GPC; x++ {
					if g.slots[x] != "" {
						ok = false
						break
					}
				}
				if ok {
					out = append(out, adapter.Candidate{Node: n.name, GPU: gi, Start: s, FreeGPC: free, FreeMemory: fm})
				}
			}
		}
	}
	return out, totalFreeGPC >= p.GPC && totalFreeMem >= p.MemoryGB
}

func matches(sel, labels map[string]string) bool {
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}
func (e *engine) nodesByName(name string) *node {
	for i := range e.nodes {
		if e.nodes[i].name == name {
			return &e.nodes[i]
		}
	}
	panic("unknown node")
}
func (e *engine) release(a allocation) {
	g := &e.nodesByName(a.node).gpus[a.gpu]
	for i, x := range g.slots {
		if x == a.job.ID {
			g.slots[i] = ""
		}
	}
	g.memoryUsed -= a.profile.MemoryGB
	e.deletes++
	e.emit("finished", a.job, a.node, a.gpu, "")
}

func (e *engine) integrate(dt int64) {
	if dt <= 0 {
		return
	}
	usedG, usedM, totalG, totalM := 0, 0, 0, 0
	frag, stranded, physicalGPC, strandedGPC := e.fragmentation()
	for _, j := range e.pending {
		p, ok := e.resolveProfile(j)
		if !ok {
			continue
		}
		cs, totalEnough := e.candidates(j, p)
		if len(cs) == 0 && totalEnough {
			e.blockedGPCMS += int64(p.GPC) * dt
			memoryMB := j.MemoryMB
			if memoryMB == 0 {
				memoryMB = p.MemoryGB * 1024
			}
			e.blockedMemoryMBMS += int64(memoryMB) * dt
		}
	}
	for ni := range e.nodes {
		for gi := range e.nodes[ni].gpus {
			g := &e.nodes[ni].gpus[gi]
			totalG += len(g.slots)
			for _, x := range g.slots {
				if x != "" {
					usedG++
				}
			}
			usedM += g.memoryUsed
			totalM += e.c.Cluster.MemoryGB
		}
	}
	measurementStart := e.c.Limits.MeasurementStartMS
	from, to := e.now, e.now+dt
	if from < measurementStart {
		from = measurementStart
	}
	if to > e.lastArrival {
		to = e.lastArrival
	}
	if to > from {
		window := to - from
		e.measurementMS += window
		e.gpcArea += float64(usedG*int(window)) / float64(totalG)
		e.memArea += float64(usedM*int(window)) / float64(totalM)
		e.fragArea += frag * float64(window)
		e.strandedArea += stranded * float64(window)
		e.physicalGPCMS += int64(physicalGPC) * window
		e.strandedGPCMS += int64(strandedGPC) * window
		if len(e.pending) > 0 {
			e.backlogMS += window
		}
		util := float64(usedG) / float64(totalG)
		if util > e.peakGPCUtilization {
			e.peakGPCUtilization = util
		}
	}
}

func (e *engine) fragmentation() (float64, float64, int, int) {
	var pf, sf float64
	physicalGPC, strandedGPC := 0, 0
	count := 0
	for ni := range e.nodes {
		for gi := range e.nodes[ni].gpus {
			g := &e.nodes[ni].gpus[gi]
			free := 0
			maxrun, run := 0, 0
			for _, x := range g.slots {
				if x == "" {
					free++
					run++
					if run > maxrun {
						maxrun = run
					}
				} else {
					run = 0
				}
			}
			if free > 0 {
				pf += float64(free-maxrun) / float64(free)
				pack := e.maxPack(g)
				sf += float64(free-pack) / float64(free)
				physicalGPC += free - maxrun
				strandedGPC += free - pack
			}
			count++
		}
	}
	if count == 0 {
		return 0, 0, 0, 0
	}
	return pf / float64(count), sf / float64(count), physicalGPC, strandedGPC
}

func (e *engine) maxPack(g *gpu) int {
	slots := append([]string(nil), g.slots...)
	mem := e.c.Cluster.MemoryGB - g.memoryUsed
	best := 0
	var rec func(int, int)
	rec = func(pos, used int) {
		if used > best {
			best = used
		}
		for _, p := range e.c.Cluster.Profiles {
			if p.MemoryGB > mem {
				continue
			}
			starts := p.Placements
			if len(starts) == 0 {
				for s := 0; s+p.GPC <= len(slots); s++ {
					starts = append(starts, s)
				}
			}
			for _, s := range starts {
				if s < pos || s+p.GPC > len(slots) {
					continue
				}
				ok := true
				for x := s; x < s+p.GPC; x++ {
					if slots[x] != "" {
						ok = false
					}
				}
				if !ok {
					continue
				}
				for x := s; x < s+p.GPC; x++ {
					slots[x] = "#"
				}
				mem -= p.MemoryGB
				rec(s, used+p.GPC)
				mem += p.MemoryGB
				for x := s; x < s+p.GPC; x++ {
					slots[x] = ""
				}
			}
		}
	}
	rec(0, 0)
	return best
}

func (e *engine) emit(kind string, j model.Job, n string, g int, msg string) {
	pf, sf, _, _ := e.fragmentation()
	e.events = append(e.events, model.Event{TimeMS: e.now, WallTime: model.Now(), Type: kind, Backend: e.backend.Name, JobID: j.ID, Node: n, GPU: g, Profile: j.Profile, QueueDepth: len(e.pending), PhysicalFrag: pf, Stranded: sf, Message: msg})
}

func (e *engine) summary(total int) model.Summary {
	s := model.Summary{Backend: e.backend.Name, Scenario: e.c.Name, Seed: e.c.Workload.Seed, Jobs: total, Completed: len(e.results), MakespanMS: e.now, MIGCreates: e.creates, MIGDeletes: e.deletes, MIGReconfigures: e.reconfigs, RequestFragmentationCount: e.reqFrag, PeakGPCUtilization: e.peakGPCUtilization, MeasurementDurationMS: e.measurementMS}
	s.PhysicalFragmentedGPCMS = e.physicalGPCMS
	s.StrandedGPCMS = e.strandedGPCMS
	s.FragmentationBlockedGPCMS = e.blockedGPCMS
	s.FragmentationBlockedMemMBMS = e.blockedMemoryMBMS
	if e.measurementMS > 0 {
		s.BackloggedTimeFraction = float64(e.backlogMS) / float64(e.measurementMS)
	}
	if total > 0 {
		s.SuccessRate = float64(len(e.results)) / float64(total)
	}
	if e.now > 0 {
		s.ThroughputPerVirtualHour = float64(len(e.results)) * 3600000 / float64(e.now)
	}
	if e.measurementMS > 0 {
		s.GPCUtilization = e.gpcArea / float64(e.measurementMS)
		s.MemoryUtilization = e.memArea / float64(e.measurementMS)
		s.MeanPhysicalFragmentation = e.fragArea / float64(e.measurementMS)
		s.MeanStrandedCapacity = e.strandedArea / float64(e.measurementMS)
		totalGPC := e.c.Cluster.Nodes * e.c.Cluster.GPUsPerNode * e.c.Cluster.GPCPerGPU
		s.StrandedGPCTimeRatio = float64(s.StrandedGPCMS) / float64(int64(totalGPC)*e.measurementMS)
	}
	for _, r := range e.results {
		s.OfferedGPCMS += int64(profileGPC(e.c.Cluster.Profiles, r.Profile)) * r.RuntimeMS
	}
	if s.OfferedGPCMS > 0 {
		s.BlockedDemandRatio = float64(s.FragmentationBlockedGPCMS) / float64(s.OfferedGPCMS)
	}
	s.HighLoadValid = s.GPCUtilization >= e.c.Limits.MinAverageGPCUtilization && s.BackloggedTimeFraction >= e.c.Limits.MinBacklogFraction
	if !s.HighLoadValid {
		s.HighLoadFailure = fmt.Sprintf("average GPC utilization %.3f < %.3f or backlog fraction %.3f < %.3f", s.GPCUtilization, e.c.Limits.MinAverageGPCUtilization, s.BackloggedTimeFraction, e.c.Limits.MinBacklogFraction)
	}
	waits := make([]int64, 0, len(e.results))
	var sw, sl int64
	for _, r := range e.results {
		waits = append(waits, r.WaitMS)
		sw += r.WaitMS
		sl += r.SchedulingUS
	}
	if len(waits) > 0 {
		s.MeanWaitMS = float64(sw) / float64(len(waits))
		s.MeanSchedulingUS = float64(sl) / float64(len(waits))
		s.P50WaitMS = quantile(waits, .5)
		s.P95WaitMS = quantile(waits, .95)
		s.P99WaitMS = quantile(waits, .99)
	}
	return s
}

func profileGPC(profiles []model.Profile, name string) int {
	for _, p := range profiles {
		if p.Name == name {
			return p.GPC
		}
	}
	return 0
}
func quantile(v []int64, q float64) float64 {
	v = append([]int64(nil), v...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	if len(v) == 0 {
		return 0
	}
	x := q * float64(len(v)-1)
	lo := int(x)
	hi := lo + 1
	if hi >= len(v) {
		return float64(v[lo])
	}
	return float64(v[lo]) + (x-float64(lo))*float64(v[hi]-v[lo])
}
