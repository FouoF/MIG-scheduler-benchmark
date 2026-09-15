package cli

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/dynamia-ai/migbench/internal/agent"
	"github.com/dynamia-ai/migbench/internal/config"
	"github.com/dynamia-ai/migbench/internal/controlplane"
	"github.com/dynamia-ai/migbench/internal/model"
	"github.com/dynamia-ai/migbench/internal/simulator"
	"github.com/dynamia-ai/migbench/internal/workload"
)

func Generate(args []string) error {
	f := flag.NewFlagSet("generate", flag.ContinueOnError)
	cp := f.String("config", "experiment.yaml", "experiment config")
	out := f.String("out", "trace.jsonl", "output trace")
	if err := f.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*cp)
	if err != nil {
		return err
	}
	jobs, err := workload.Generate(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil && filepath.Dir(*out) != "." {
		return err
	}
	if err := workload.Write(*out, jobs); err != nil {
		return err
	}
	fmt.Printf("generated %d jobs in %s (seed=%d)\n", len(jobs), *out, c.Workload.Seed)
	return nil
}

func Run(args []string) error {
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	cp := f.String("config", "experiment.yaml", "experiment config")
	tp := f.String("trace", "", "JSONL trace; generated if omitted")
	out := f.String("out", "results", "result directory")
	only := f.String("backend", "", "run only this backend name")
	if err := f.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*cp)
	if err != nil {
		return err
	}
	selectedModes := map[string]int{}
	for _, b := range c.Backends {
		if *only == "" || b.Name == *only {
			selectedModes[b.Mode]++
		}
	}
	if len(selectedModes) > 1 {
		return fmt.Errorf("refusing to mix simulated and control-plane results; select one backend with -backend")
	}
	if selectedModes["control-plane"] > 1 {
		return fmt.Errorf("control-plane backends require fresh isolated clusters; select one backend with -backend")
	}
	var jobs []model.Job
	if *tp == "" {
		jobs, err = workload.Generate(c)
	} else {
		jobs, err = workload.Read(*tp)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	if err := workload.Write(filepath.Join(*out, "trace.jsonl"), jobs); err != nil {
		return err
	}
	cfgRaw, _ := os.ReadFile(*cp)
	_ = os.WriteFile(filepath.Join(*out, "experiment.yaml"), cfgRaw, 0644)
	count := 0
	var invalid []string
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	for _, b := range c.Backends {
		if *only != "" && b.Name != *only {
			continue
		}
		dir := filepath.Join(*out, safe(b.Name))
		var summary model.Summary
		if b.Mode == "control-plane" {
			api := agent.Kubectl{Path: b.Parameters["kubectl"], Context: b.Parameters["kubeContext"]}
			r, runErr := controlplane.Run(ctx, c, b, jobs, api)
			if runErr != nil {
				_ = writeArtifacts(dir, r.Summary, r.Events, r.Jobs, r.Objects, b)
				_ = os.WriteFile(filepath.Join(dir, "infrastructure-error.txt"), []byte(runErr.Error()+"\n"), 0644)
				return fmt.Errorf("backend %s: %w", b.Name, runErr)
			}
			if err := writeArtifacts(dir, r.Summary, r.Events, r.Jobs, r.Objects, b); err != nil {
				return err
			}
			summary = r.Summary
		} else {
			r, runErr := simulator.Run(c, b, jobs)
			if runErr != nil {
				return fmt.Errorf("backend %s: %w", b.Name, runErr)
			}
			if err := writeResult(dir, r, b); err != nil {
				return err
			}
			summary = r.Summary
		}
		fmt.Printf("%-20s completed=%d makespan=%dms mean-wait=%.1fms throughput=%.2f/h peak=%.1f%% backlog=%.1f%% valid=%t\n", b.Name, summary.Completed, summary.MakespanMS, summary.MeanWaitMS, summary.ThroughputPerVirtualHour, summary.PeakGPCUtilization*100, summary.BackloggedTimeFraction*100, summary.HighLoadValid)
		if c.Limits.RequireHighLoad && !summary.HighLoadValid {
			invalid = append(invalid, b.Name+": "+summary.HighLoadFailure)
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("no backend matched %q", *only)
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid non-saturated run: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func writeResult(dir string, r simulator.Result, b config.Backend) error {
	return writeArtifacts(dir, r.Summary, r.Events, r.Jobs, r.Objects, b)
}

func writeArtifacts(dir string, summary model.Summary, events []model.Event, jobs []model.JobResult, objects map[string]string, b config.Backend) error {
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "summary.json"), summary); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "events.jsonl"), events); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "jobs.jsonl"), jobs); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "backend.lock.json"), b); err != nil {
		return err
	}
	for id, raw := range objects {
		if err := os.WriteFile(filepath.Join(dir, "objects", safe(id)+".json"), []byte(raw), 0644); err != nil {
			return err
		}
	}
	return nil
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func writeJSONL[T any](path string, vs []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	e := json.NewEncoder(f)
	for _, v := range vs {
		if err := e.Encode(v); err != nil {
			return err
		}
	}
	return nil
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func Compare(args []string) error {
	f := flag.NewFlagSet("compare", flag.ContinueOnError)
	in := f.String("in", "results", "results directory")
	out := f.String("out", "report.html", "HTML report")
	if err := f.Parse(args); err != nil {
		return err
	}
	var sums []model.Summary
	err := filepath.WalkDir(*in, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "summary.json" {
			var s model.Summary
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(b, &s); e != nil {
				return e
			}
			sums = append(sums, s)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(sums) == 0 {
		return fmt.Errorf("no summary.json files found in %s", *in)
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].Backend < sums[j].Backend })
	if err := writeSummaryCSV(filepath.Join(filepath.Dir(*out), "summary.csv"), sums); err != nil {
		return err
	}
	data, _ := json.Marshal(sums)
	fout, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer fout.Close()
	return reportTemplate.Execute(fout, map[string]any{"Rows": sums, "Data": template.JS(data)})
}

func writeSummaryCSV(path string, s []model.Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"backend", "jobs", "completed", "makespan_ms", "throughput_per_hour", "mean_wait_ms", "p95_wait_ms", "gpc_utilization", "memory_utilization", "physical_fragmentation", "stranded_capacity", "request_fragmentation", "mig_creates", "mig_deletes", "peak_gpc_utilization", "backlogged_time_fraction", "high_load_valid"})
	for _, x := range s {
		_ = w.Write([]string{x.Backend, strconv.Itoa(x.Jobs), strconv.Itoa(x.Completed), strconv.FormatInt(x.MakespanMS, 10), ff(x.ThroughputPerVirtualHour), ff(x.MeanWaitMS), ff(x.P95WaitMS), ff(x.GPCUtilization), ff(x.MemoryUtilization), ff(x.MeanPhysicalFragmentation), ff(x.MeanStrandedCapacity), strconv.Itoa(x.RequestFragmentationCount), strconv.Itoa(x.MIGCreates), strconv.Itoa(x.MIGDeletes), ff(x.PeakGPCUtilization), ff(x.BackloggedTimeFraction), strconv.FormatBool(x.HighLoadValid)})
	}
	return w.Error()
}
func ff(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

func Inspect(args []string) error {
	f := flag.NewFlagSet("inspect", flag.ContinueOnError)
	ep := f.String("events", "", "events.jsonl")
	job := f.String("job", "", "optional job id")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *ep == "" {
		return fmt.Errorf("-events is required")
	}
	in, err := os.Open(*ep)
	if err != nil {
		return err
	}
	defer in.Close()
	s := bufio.NewScanner(in)
	for s.Scan() {
		var e model.Event
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return err
		}
		if *job == "" || e.JobID == *job {
			fmt.Printf("%9dms  %-10s %-12s node=%-12s gpu=%d queue=%d frag=%.3f %s\n", e.TimeMS, e.Type, e.JobID, e.Node, e.GPU, e.QueueDepth, e.PhysicalFrag, e.Message)
		}
	}
	return s.Err()
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{"pct": func(v float64) float64 { return v * 100 }}).Parse(`<!doctype html><html><head><meta charset="utf-8"><title>MIGBench Report</title><style>body{font:14px system-ui;margin:32px;color:#18212f}h1{margin-bottom:4px}.sub{color:#667085;margin-bottom:24px}table{border-collapse:collapse;width:100%;box-shadow:0 1px 4px #0002}th,td{padding:10px;border-bottom:1px solid #e5e7eb;text-align:right}th{background:#f3f5f8}th:first-child,td:first-child{text-align:left}.cards{display:flex;gap:16px;margin:24px 0}.card{padding:16px;border:1px solid #ddd;border-radius:10px;flex:1}canvas{width:100%;height:300px;border:1px solid #eee;margin-top:22px}</style></head><body><h1>Dynamic MIG Scheduling Benchmark</h1><div class="sub">Self-contained paired experiment summary. Virtual-time performance and wall-clock scheduler latency are reported separately.</div><table><thead><tr><th>Backend</th><th>Completed</th><th>Makespan ms</th><th>Throughput /h</th><th>Mean wait ms</th><th>P95 wait ms</th><th>GPC util.</th><th>Memory util.</th><th>Physical frag.</th><th>Stranded</th><th>Req. frag.</th><th>Creates</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Backend}}</td><td>{{.Completed}}/{{.Jobs}}</td><td>{{.MakespanMS}}</td><td>{{printf "%.2f" .ThroughputPerVirtualHour}}</td><td>{{printf "%.1f" .MeanWaitMS}}</td><td>{{printf "%.1f" .P95WaitMS}}</td><td>{{printf "%.1f%%" (pct .GPCUtilization)}}</td><td>{{printf "%.1f%%" (pct .MemoryUtilization)}}</td><td>{{printf "%.3f" .MeanPhysicalFragmentation}}</td><td>{{printf "%.3f" .MeanStrandedCapacity}}</td><td>{{.RequestFragmentationCount}}</td><td>{{.MIGCreates}}</td></tr>{{end}}</tbody></table><canvas id="chart" width="1100" height="300"></canvas><script>const rows={{.Data}},c=document.getElementById('chart'),x=c.getContext('2d');x.font='13px system-ui';const max=Math.max(...rows.map(r=>r.meanWaitMS),1),w=c.width/(rows.length*2+1);rows.forEach((r,i)=>{const h=r.meanWaitMS/max*230,px=w*(i*2+1);x.fillStyle='#635bff';x.fillRect(px,260-h,w,h);x.fillStyle='#18212f';x.fillText(r.backend,px,282);x.fillText(Math.round(r.meanWaitMS)+' ms',px,250-h)});x.fillText('Mean queue wait',12,20)</script></body></html>`))
