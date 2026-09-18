package controlplane

import (
	"fmt"
	"sort"

	"github.com/dynamia-ai/migbench/internal/agent"
	"github.com/dynamia-ai/migbench/internal/model"
)

type gpuState struct {
	slots  [7]string
	memory int
}

func (e *engine) snapshot() map[string]*gpuState {
	out := map[string]*gpuState{}
	for _, a := range e.agents {
		for _, d := range a.State.Active {
			key := d.Node + "/" + d.GPU
			g := out[key]
			if g == nil {
				g = &gpuState{}
				out[key] = g
			}
			p := a.State.Profiles[d.Profile]
			for slot := d.Start; slot < d.Start+d.Size; slot++ {
				g.slots[slot] = d.Key
			}
			g.memory += p.MemoryMB
		}
	}
	return out
}

func (e *engine) integrate(dt int64) {
	if dt <= 0 {
		return
	}
	states := e.snapshot()
	totalGPU := len(e.nodes) * e.c.Cluster.GPUsPerNode
	usedGPC, usedMemory := 0, 0
	for _, g := range states {
		for _, owner := range g.slots {
			if owner != "" {
				usedGPC++
			}
		}
		usedMemory += g.memory
	}
	physical, stranded, physicalGPC, strandedGPC := e.fragmentation(states)
	for id := range e.pending {
		j := e.jobsByID[id]
		p, blocked := e.requestFragmentedNow(states, j)
		if blocked {
			e.blockedGPCMS += int64(p.Size) * dt
			memoryMB := j.MemoryMB
			if memoryMB == 0 {
				memoryMB = p.MemoryMB
			}
			e.blockedMemoryMBMS += int64(memoryMB) * dt
		}
	}
	from, to := e.now, e.now+dt
	if from < e.c.Limits.MeasurementStartMS {
		from = e.c.Limits.MeasurementStartMS
	}
	if to > e.lastArrival {
		to = e.lastArrival
	}
	if to <= from || totalGPU == 0 {
		return
	}
	window := to - from
	e.measurement += window
	e.gpcArea += float64(usedGPC) / float64(totalGPU*7) * float64(window)
	e.memArea += float64(usedMemory) / float64(totalGPU*81920) * float64(window)
	e.fragArea += physical * float64(window)
	e.strandArea += stranded * float64(window)
	e.physicalGPCMS += int64(physicalGPC) * window
	e.strandedGPCMS += int64(strandedGPC) * window
	if len(e.pending) > 0 {
		e.backlog += window
	}
	util := float64(usedGPC) / float64(totalGPU*7)
	if util > e.peak {
		e.peak = util
	}
}

func (e *engine) fragmentation(states map[string]*gpuState) (float64, float64, int, int) {
	profiles := agent.H100State().Profiles
	var physical, stranded float64
	physicalGPC, strandedGPC := 0, 0
	totalGPU := len(e.nodes) * e.c.Cluster.GPUsPerNode
	for _, node := range e.nodes {
		for gpu := 0; gpu < e.c.Cluster.GPUsPerNode; gpu++ {
			key := node + "/GPU-" + node + fmt.Sprintf("-%d", gpu)
			g := states[key]
			if g == nil {
				g = &gpuState{}
			}
			free, maxRun, run := 0, 0, 0
			for _, owner := range g.slots {
				if owner == "" {
					free++
					run++
					if run > maxRun {
						maxRun = run
					}
				} else {
					run = 0
				}
			}
			if free > 0 {
				physical += float64(free-maxRun) / float64(free)
				pack := maxPack(*g, profiles)
				stranded += float64(free-pack) / float64(free)
				physicalGPC += free - maxRun
				strandedGPC += free - pack
			}
		}
	}
	if totalGPU == 0 {
		return 0, 0, 0, 0
	}
	return physical / float64(totalGPU), stranded / float64(totalGPU), physicalGPC, strandedGPC
}

func maxPack(g gpuState, profiles map[string]agent.Profile) int {
	best := 0
	var rec func(int)
	rec = func(used int) {
		if used > best {
			best = used
		}
		for _, p := range profiles {
			if g.memory+p.MemoryMB > 81920 {
				continue
			}
			for start := range p.LegalStarts {
				ok := true
				for slot := start; slot < start+p.Size; slot++ {
					if g.slots[slot] != "" {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				for slot := start; slot < start+p.Size; slot++ {
					g.slots[slot] = "#"
				}
				g.memory += p.MemoryMB
				rec(used + p.Size)
				g.memory -= p.MemoryMB
				for slot := start; slot < start+p.Size; slot++ {
					g.slots[slot] = ""
				}
			}
		}
	}
	rec(0)
	return best
}

func (e *engine) markRequestFragmentation() {
	states := e.snapshot()
	for id := range e.pending {
		if e.fragmented[id] {
			continue
		}
		_, blocked := e.requestFragmentedNow(states, e.jobsByID[id])
		if blocked {
			e.fragmented[id] = true
			e.reqFrag++
		}
	}
}

func (e *engine) requestFragmentedNow(states map[string]*gpuState, j model.Job) (agent.Profile, bool) {
	totalFreeGPC, totalFreeMemory, placeable := 0, 0, false
	for _, node := range e.nodes {
		for gpu := 0; gpu < e.c.Cluster.GPUsPerNode; gpu++ {
			key := node + "/GPU-" + node + fmt.Sprintf("-%d", gpu)
			g := states[key]
			if g == nil {
				g = &gpuState{}
			}
			free := 0
			for _, owner := range g.slots {
				if owner == "" {
					free++
				}
			}
			totalFreeGPC += free
			totalFreeMemory += 81920 - g.memory
			for _, p := range agent.H100State().Profiles {
				if !profileFits(j, p) || g.memory+p.MemoryMB > 81920 {
					continue
				}
				for start := range p.LegalStarts {
					ok := true
					for slot := start; slot < start+p.Size; slot++ {
						if g.slots[slot] != "" {
							ok = false
							break
						}
					}
					if ok {
						placeable = true
					}
				}
			}
		}
	}
	minimum := minimumProfile(j)
	return minimum, !placeable && minimum.Size > 0 && totalFreeGPC >= minimum.Size && totalFreeMemory >= minimum.MemoryMB
}

func profileFits(j model.Job, p agent.Profile) bool {
	if j.Profile != "" {
		return j.Profile == p.Name
	}
	return p.MemoryMB >= j.MemoryMB && p.Compute >= j.MinComputePercent
}

func minimumProfile(j model.Job) agent.Profile {
	var out agent.Profile
	for _, p := range agent.H100State().Profiles {
		if profileFits(j, p) && (out.Size == 0 || p.Size < out.Size || p.Size == out.Size && p.MemoryMB < out.MemoryMB) {
			out = p
		}
	}
	return out
}

func (e *engine) emit(kind string, j model.Job, d agent.Decision, message string) {
	physical, stranded, _, _ := e.fragmentation(e.snapshot())
	e.events = append(e.events, model.Event{TimeMS: e.now, WallTime: model.Now(), Type: kind, Backend: e.b.Name, JobID: j.ID, Node: d.Node, GPU: gpuIndex(d.GPU), Profile: d.Profile, QueueDepth: len(e.pending), PhysicalFrag: physical, Stranded: stranded, Message: message})
}

func (e *engine) summary() model.Summary {
	s := model.Summary{Backend: e.b.Name, Scenario: e.c.Name, Seed: e.c.Workload.Seed, Jobs: len(e.jobs), Completed: len(e.results), MakespanMS: e.now, MIGCreates: e.creates, MIGDeletes: e.deletes, RequestFragmentationCount: e.reqFrag, Timeouts: e.timeouts, PeakGPCUtilization: e.peak, MeasurementDurationMS: e.measurement}
	s.PhysicalFragmentedGPCMS = e.physicalGPCMS
	s.StrandedGPCMS = e.strandedGPCMS
	s.FragmentationBlockedGPCMS = e.blockedGPCMS
	s.FragmentationBlockedMemMBMS = e.blockedMemoryMBMS
	if len(e.jobs) > 0 {
		s.SuccessRate = float64(len(e.results)) / float64(len(e.jobs))
	}
	if e.now > 0 {
		s.ThroughputPerVirtualHour = float64(len(e.results)) * 3600000 / float64(e.now)
	}
	if e.measurement > 0 {
		s.GPCUtilization = e.gpcArea / float64(e.measurement)
		s.MemoryUtilization = e.memArea / float64(e.measurement)
		s.MeanPhysicalFragmentation = e.fragArea / float64(e.measurement)
		s.MeanStrandedCapacity = e.strandArea / float64(e.measurement)
		s.BackloggedTimeFraction = float64(e.backlog) / float64(e.measurement)
		totalGPC := len(e.nodes) * e.c.Cluster.GPUsPerNode * e.c.Cluster.GPCPerGPU
		s.StrandedGPCTimeRatio = float64(s.StrandedGPCMS) / float64(int64(totalGPC)*e.measurement)
	}
	for _, j := range e.jobs {
		p := minimumProfile(j)
		if p.Size == 0 {
			continue
		}
		runtime := j.DurationMS
		if j.ComputeCoreMS > 0 {
			runtime = (j.ComputeCoreMS + int64(p.Compute) - 1) / int64(p.Compute)
		}
		s.OfferedGPCMS += int64(p.Size) * runtime
	}
	if s.OfferedGPCMS > 0 {
		s.BlockedDemandRatio = float64(s.FragmentationBlockedGPCMS) / float64(s.OfferedGPCMS)
	}
	s.HighLoadValid = s.GPCUtilization >= e.c.Limits.MinAverageGPCUtilization && s.BackloggedTimeFraction >= e.c.Limits.MinBacklogFraction
	if !s.HighLoadValid {
		s.HighLoadFailure = fmt.Sprintf("average GPC utilization %.3f < %.3f or backlog fraction %.3f < %.3f", s.GPCUtilization, e.c.Limits.MinAverageGPCUtilization, s.BackloggedTimeFraction, e.c.Limits.MinBacklogFraction)
	}
	waits := make([]int64, 0, len(e.results))
	var waitSum, scheduleSum int64
	for _, r := range e.results {
		waits = append(waits, r.WaitMS)
		waitSum += r.WaitMS
		scheduleSum += r.SchedulingUS
	}
	if len(waits) > 0 {
		s.MeanWaitMS = float64(waitSum) / float64(len(waits))
		s.MeanSchedulingUS = float64(scheduleSum) / float64(len(waits))
		s.P50WaitMS = quantile(waits, .50)
		s.P95WaitMS = quantile(waits, .95)
		s.P99WaitMS = quantile(waits, .99)
	}
	return s
}

func quantile(values []int64, q float64) float64 {
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	if len(v) == 0 {
		return 0
	}
	x := q * float64(len(v)-1)
	lo, hi := int(x), int(x)+1
	if hi >= len(v) {
		return float64(v[lo])
	}
	return float64(v[lo]) + (x-float64(lo))*float64(v[hi]-v[lo])
}
