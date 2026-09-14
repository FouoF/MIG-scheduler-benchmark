package model

import "time"

type Profile struct {
	Name           string `json:"name" yaml:"name"`
	GPC            int    `json:"gpc" yaml:"gpc"`
	MemoryGB       int    `json:"memoryGB" yaml:"memoryGB"`
	ComputePercent int    `json:"computePercent" yaml:"computePercent"`
	Placements     []int  `json:"placements,omitempty" yaml:"placements,omitempty"`
}

type Job struct {
	ID                string            `json:"jobID" yaml:"jobID"`
	Format            string            `json:"format,omitempty" yaml:"format,omitempty"`
	ArrivalMS         int64             `json:"arrivalMS" yaml:"arrivalMS"`
	ComputeCoreMS     int64             `json:"computeCoreMS,omitempty" yaml:"computeCoreMS,omitempty"`
	MemoryMB          int               `json:"memoryMB,omitempty" yaml:"memoryMB,omitempty"`
	MinComputePercent int               `json:"minComputePercent,omitempty" yaml:"minComputePercent,omitempty"`
	DeadlineMS        int64             `json:"deadlineMS,omitempty" yaml:"deadlineMS,omitempty"`
	DurationMS        int64             `json:"durationMS,omitempty" yaml:"durationMS,omitempty"`
	Profile           string            `json:"profile,omitempty" yaml:"profile,omitempty"`
	NodeSelector      map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
}

type PlacementDecision struct {
	JobID     string `json:"jobID"`
	Node      string `json:"node,omitempty"`
	GPU       int    `json:"gpu,omitempty"`
	StartSlot int    `json:"startSlot,omitempty"`
	Profile   string `json:"profile"`
	Accepted  bool   `json:"accepted"`
	Reason    string `json:"reason,omitempty"`
	LatencyUS int64  `json:"latencyUS"`
}

type Event struct {
	TimeMS       int64   `json:"timeMS"`
	WallTime     string  `json:"wallTime,omitempty"`
	Type         string  `json:"type"`
	Backend      string  `json:"backend"`
	JobID        string  `json:"jobID,omitempty"`
	Node         string  `json:"node,omitempty"`
	GPU          int     `json:"gpu,omitempty"`
	Profile      string  `json:"profile,omitempty"`
	QueueDepth   int     `json:"queueDepth"`
	PhysicalFrag float64 `json:"physicalFragmentation"`
	Stranded     float64 `json:"strandedCapacity"`
	Message      string  `json:"message,omitempty"`
}

type JobResult struct {
	JobID             string `json:"jobID"`
	Profile           string `json:"profile"`
	ArrivalMS         int64  `json:"arrivalMS"`
	StartMS           int64  `json:"startMS"`
	FinishMS          int64  `json:"finishMS"`
	WaitMS            int64  `json:"waitMS"`
	SchedulingUS      int64  `json:"schedulingUS"`
	Node              string `json:"node"`
	GPU               int    `json:"gpu"`
	RequestFragmented bool   `json:"requestFragmented"`
	ComputeCoreMS     int64  `json:"computeCoreMS,omitempty"`
	RequestedMemoryMB int    `json:"requestedMemoryMB,omitempty"`
	AllocatedMemoryMB int    `json:"allocatedMemoryMB,omitempty"`
	AllocatedCompute  int    `json:"allocatedComputePercent,omitempty"`
	RuntimeMS         int64  `json:"runtimeMS"`
}

type Summary struct {
	Backend                   string  `json:"backend"`
	Seed                      int64   `json:"seed"`
	Jobs                      int     `json:"jobs"`
	Completed                 int     `json:"completed"`
	MakespanMS                int64   `json:"makespanMS"`
	ThroughputPerVirtualHour  float64 `json:"throughputPerVirtualHour"`
	MeanWaitMS                float64 `json:"meanWaitMS"`
	P50WaitMS                 float64 `json:"p50WaitMS"`
	P95WaitMS                 float64 `json:"p95WaitMS"`
	P99WaitMS                 float64 `json:"p99WaitMS"`
	SuccessRate               float64 `json:"successRate"`
	GPCUtilization            float64 `json:"gpcUtilization"`
	MemoryUtilization         float64 `json:"memoryUtilization"`
	MeanPhysicalFragmentation float64 `json:"meanPhysicalFragmentation"`
	MeanStrandedCapacity      float64 `json:"meanStrandedCapacity"`
	RequestFragmentationCount int     `json:"requestFragmentationCount"`
	MIGCreates                int     `json:"migCreates"`
	MIGDeletes                int     `json:"migDeletes"`
	MIGReconfigures           int     `json:"migReconfigures"`
	ReconfigureTimeMS         int64   `json:"reconfigureTimeMS"`
	MeanSchedulingUS          float64 `json:"meanSchedulingUS"`
	Timeouts                  int     `json:"timeouts"`
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
