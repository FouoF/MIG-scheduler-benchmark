.PHONY: build test e2e
build:
	mkdir -p bin
	go build -o bin/migbench ./cmd/migbench
test:
	go test ./...
e2e: build
	mkdir -p work
	./bin/migbench generate -config experiment.example.yaml -out work/trace.jsonl
	./bin/migbench run -config experiment.example.yaml -trace work/trace.jsonl -out work/results
	./bin/migbench compare -in work/results -out work/results/report.html
