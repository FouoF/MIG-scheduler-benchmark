package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dynamia-ai/migbench/internal/adapter"
	"github.com/dynamia-ai/migbench/internal/agent"
	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/model"
)

type Result struct {
	Summary model.Summary     `json:"summary"`
	Jobs    []model.JobResult `json:"jobs"`
	Events  []model.Event     `json:"events"`
	Objects map[string]string `json:"objects"`
}

type allocation struct {
	job      model.Job
	decision agent.Decision
	profile  agent.Profile
	finish   int64
}

type engine struct {
	c           config.Config
	b           config.Backend
	api         agent.API
	adapter     adapter.Adapter
	agents      []*agent.Agent
	nodes       []string
	jobs        []model.Job
	jobsByID    map[string]model.Job
	submitted   map[string]time.Time
	pending     map[string]bool
	running     map[string]allocation
	results     []model.JobResult
	events      []model.Event
	objects     map[string]string
	fragmented  map[string]bool
	now         int64
	lastArrival int64
	last        int64
	creates     int
	deletes     int
	reqFrag     int
	timeouts    int
	measurement int64
	backlog     int64
	gpcArea     float64
	memArea     float64
	fragArea    float64
	strandArea  float64
	peak        float64
	settle      time.Duration
	poll        time.Duration
	namespace   string
}

func Run(ctx context.Context, c config.Config, b config.Backend, jobs []model.Job, api agent.API) (Result, error) {
	if b.Kind != "hami" && b.Kind != "nvidia-dra" {
		return Result{}, fmt.Errorf("backend %s does not support control-plane mode", b.Name)
	}
	nodes := splitNonEmpty(b.Parameters["nodes"])
	if len(nodes) != c.Cluster.Nodes {
		return Result{}, fmt.Errorf("backend %s has %d parameters.nodes, cluster.nodes is %d", b.Name, len(nodes), c.Cluster.Nodes)
	}
	seen := map[string]bool{}
	jobs = append([]model.Job(nil), jobs...)
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ArrivalMS < jobs[j].ArrivalMS })
	for _, j := range jobs {
		if j.ID == "" || seen[j.ID] {
			return Result{}, fmt.Errorf("job IDs must be non-empty and unique: %q", j.ID)
		}
		if j.ArrivalMS < 0 || (j.Profile == "" && (j.ComputeCoreMS < 1 || j.MemoryMB < 1)) || (j.Profile != "" && j.DurationMS < 1) {
			return Result{}, fmt.Errorf("job %s has invalid timing or resources", j.ID)
		}
		seen[j.ID] = true
	}
	e := &engine{c: c, b: b, api: api, adapter: adapter.New(b), nodes: nodes, jobs: jobs, jobsByID: map[string]model.Job{}, submitted: map[string]time.Time{}, pending: map[string]bool{}, running: map[string]allocation{}, objects: map[string]string{}, fragmented: map[string]bool{}, settle: parameterDuration(b, "settleMS", 500*time.Millisecond), poll: parameterDuration(b, "pollMS", 50*time.Millisecond), namespace: b.Parameters["namespace"]}
	if e.namespace == "" {
		e.namespace = "default"
	}
	if len(jobs) > 0 {
		e.lastArrival = jobs[len(jobs)-1].ArrivalMS
	}
	for _, j := range jobs {
		e.jobsByID[j.ID] = j
	}
	for _, node := range nodes {
		a, err := agent.New(b.Kind, node, api)
		if err != nil {
			return Result{}, err
		}
		a.GPUs = c.Cluster.GPUsPerNode
		a.OnEvent = func(ev agent.Event) {
			if ev.Type == "invalid" {
				e.events = append(e.events, model.Event{TimeMS: e.now, WallTime: ev.Time, Type: "backend-error", Backend: b.Name, Message: ev.Message, QueueDepth: len(e.pending)})
			}
		}
		if err := a.Publish(ctx); err != nil {
			return Result{}, fmt.Errorf("publish fake inventory for %s: %w", node, err)
		}
		e.agents = append(e.agents, a)
	}
	// HAMi's scheduler keeps its own node/device cache and learns fake inventory
	// on a polling cycle. Keep that warm-up outside virtual time and per-job
	// scheduling latency measurements.
	if wait := registrationWait(b); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	return e.run(ctx)
}

func (e *engine) run(ctx context.Context) (Result, error) {
	idx := 0
	for idx < len(e.jobs) || len(e.pending) > 0 || len(e.running) > 0 {
		next := maxInt64
		if idx < len(e.jobs) {
			next = e.jobs[idx].ArrivalMS
		}
		for _, a := range e.running {
			if a.finish < next {
				next = a.finish
			}
		}
		if next == maxInt64 {
			return e.result(), fmt.Errorf("control-plane stalled with %d pending jobs", len(e.pending))
		}
		e.integrate(next - e.now)
		e.now = next
		for id, a := range e.running {
			if a.finish == e.now {
				if err := e.deleteJob(ctx, id); err != nil {
					return e.result(), fmt.Errorf("finish %s: %w", id, err)
				}
				delete(e.running, id)
				e.deletes++
				e.emit("finished", a.job, a.decision, "")
			}
		}
		for idx < len(e.jobs) && e.jobs[idx].ArrivalMS == e.now {
			j := e.jobs[idx]
			if err := e.submit(ctx, j); err != nil {
				return e.result(), fmt.Errorf("submit %s: %w", j.ID, err)
			}
			e.pending[j.ID] = true
			e.submitted[j.ID] = time.Now()
			e.emit("arrival", j, agent.Decision{}, "")
			idx++
		}
		attempt := time.Now()
		if err := e.settleAllocations(ctx, attempt); err != nil {
			return e.result(), err
		}
		e.markRequestFragmentation()
		if len(e.running) == 0 && idx >= len(e.jobs) && len(e.pending) > 0 {
			e.timeouts += len(e.pending)
			return e.result(), fmt.Errorf("%d jobs remain unallocated after scheduler settled; first=%s", len(e.pending), firstKey(e.pending))
		}
	}
	return e.result(), nil
}

func (e *engine) result() Result {
	return Result{Summary: e.summary(), Jobs: e.results, Events: e.events, Objects: e.objects}
}

func (e *engine) submit(ctx context.Context, j model.Job) error {
	raw, err := e.adapter.Objects(j)
	if err != nil {
		return err
	}
	e.objects[j.ID] = string(raw)
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	objects, ok := value.([]any)
	if !ok {
		objects = []any{value}
	}
	for _, item := range objects {
		obj, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("adapter returned non-object")
		}
		metadata, _ := obj["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			obj["metadata"] = metadata
		}
		metadata["namespace"] = e.namespace
		if err := e.api.Apply(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) deleteJob(ctx context.Context, id string) error {
	// The simulated completion is instantaneous in virtual time, but the API
	// object must be fully gone before capacity can be reused. Otherwise kubelet
	// admission and a scheduler cache may briefly count different generations
	// of the same slot, which leaks a deletion race into placement results.
	args := []string{"delete", "pod", id, "-n", e.namespace, "--ignore-not-found=true", "--grace-period=0", "--force", "--wait=true"}
	if _, err := e.api.Run(ctx, nil, args...); err != nil {
		return err
	}
	if e.b.Kind == "nvidia-dra" {
		_, err := e.api.Run(ctx, nil, "delete", "resourceclaim", id, "-n", e.namespace, "--ignore-not-found=true", "--wait=true")
		return err
	}
	return nil
}

func (e *engine) settleAllocations(ctx context.Context, attempt time.Time) error {
	quietSince := time.Now()
	lastSignature := ""
	deadline := time.Now().Add(time.Duration(e.c.Limits.SchedulingTimeoutMS) * time.Millisecond)
	for {
		for _, a := range e.agents {
			if err := a.Reconcile(ctx); err != nil {
				return fmt.Errorf("fake agent reconcile on %s: %w", a.Node, err)
			}
		}
		sig := e.signature()
		if sig != lastSignature {
			lastSignature = sig
			quietSince = time.Now()
			if err := e.acceptNewAllocations(attempt); err != nil {
				return err
			}
		}
		if time.Since(quietSince) >= e.settle && (len(e.running) > 0 || len(e.pending) == 0) {
			return nil
		}
		if len(e.running) == 0 && len(e.pending) > 0 && time.Now().After(deadline) {
			e.timeouts += len(e.pending)
			return fmt.Errorf("scheduler decision timeout after %dms", e.c.Limits.SchedulingTimeoutMS)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.poll):
		}
	}
}

func (e *engine) acceptNewAllocations(attempt time.Time) error {
	var decisions []agent.Decision
	for _, a := range e.agents {
		for _, d := range a.State.Active {
			if e.pending[jobFromKey(d.Key)] {
				decisions = append(decisions, d)
			}
		}
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Key < decisions[j].Key })
	for _, d := range decisions {
		id := jobFromKey(d.Key)
		if !e.pending[id] {
			continue
		}
		j, ok := e.jobsByID[id]
		if !ok {
			return fmt.Errorf("scheduler allocated unknown job %q", id)
		}
		p, ok := agent.H100State().Profiles[d.Profile]
		if !ok {
			return fmt.Errorf("scheduler selected unknown profile %s", d.Profile)
		}
		if (j.Profile != "" && d.Profile != j.Profile) || p.MemoryMB < j.MemoryMB {
			return fmt.Errorf("scheduler under-allocated job %s: requested profile=%s memory=%dMi, got %s memory=%dMi", id, j.Profile, j.MemoryMB, p.Name, p.MemoryMB)
		}
		runtime := j.DurationMS
		if j.ComputeCoreMS > 0 {
			runtime = (j.ComputeCoreMS + int64(p.Compute) - 1) / int64(p.Compute)
		}
		start := e.now + e.c.Costs.CreateMS
		finish := start + runtime + e.c.Costs.DeleteMS
		gpu := gpuIndex(d.GPU)
		latency := time.Since(attempt).Microseconds()
		e.running[id] = allocation{job: j, decision: d, profile: p, finish: finish}
		delete(e.pending, id)
		e.creates++
		e.results = append(e.results, model.JobResult{JobID: id, Profile: d.Profile, ArrivalMS: j.ArrivalMS, StartMS: start, FinishMS: finish, WaitMS: start - j.ArrivalMS, SchedulingUS: latency, Node: d.Node, GPU: gpu, RequestFragmented: e.fragmented[id], ComputeCoreMS: j.ComputeCoreMS, RequestedMemoryMB: j.MemoryMB, AllocatedMemoryMB: p.MemoryMB, AllocatedCompute: p.Compute, RuntimeMS: runtime})
		e.emit("scheduled", j, d, fmt.Sprintf("slot=%d", d.Start))
	}
	return nil
}

func (e *engine) signature() string {
	var keys []string
	for _, a := range e.agents {
		for key, d := range a.State.Active {
			keys = append(keys, fmt.Sprintf("%s=%s/%s/%d", key, d.Node, d.GPU, d.Start))
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

const maxInt64 = int64(^uint64(0) >> 1)

func splitNonEmpty(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parameterDuration(b config.Backend, key string, fallback time.Duration) time.Duration {
	v, err := strconv.ParseInt(b.Parameters[key], 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return time.Duration(v) * time.Millisecond
}

func registrationWait(b config.Backend) time.Duration {
	fallback := time.Duration(0)
	if b.Kind == "hami" {
		fallback = 16 * time.Second
	}
	raw, ok := b.Parameters["registrationWaitMS"]
	if !ok {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return fallback
	}
	return time.Duration(v) * time.Millisecond
}

func jobFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) < 2 {
		return key
	}
	return parts[len(parts)-2]
}

func gpuIndex(id string) int {
	i := strings.LastIndexByte(id, '-')
	if i < 0 {
		return -1
	}
	v, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return -1
	}
	return v
}

func firstKey(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
