package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dynamia-ai/migbench/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "generate":
		err = cli.Generate(os.Args[2:])
	case "run":
		err = cli.Run(os.Args[2:])
	case "compare":
		err = cli.Compare(os.Args[2:])
	case "inspect":
		err = cli.Inspect(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `migbench - reproducible Dynamic MIG scheduling benchmark

Usage:
  migbench generate -config experiment.yaml -out trace.jsonl
  migbench run      -config experiment.yaml -trace trace.jsonl -out results
  migbench compare  -in results -out report.html
  migbench inspect  -events results/hami/events.jsonl [-job job-000001]
`)
}
