.DEFAULT_GOAL := help
.PHONY: help run test test-race build fmt vet generate-ebpf build-sensor docker-build docker-sensor kind-deploy clean
help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; printf "ContextSLO developer commands\n\n"} /^[a-zA-Z_-]+:.*?## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
run: ## Run the API and dashboard
	go run ./cmd/contextslo serve --data ./data/state.json
test: ## Run all unit and API tests
	go test ./...
test-race: ## Run tests with the race detector
	go test -race ./...
build: ## Build the server, CLI, canary, and operator binary
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/contextslo ./cmd/contextslo
fmt: ## Format Go source
	gofmt -w cmd internal
vet: ## Run Go static analysis
	go vet ./...
generate-ebpf: ## Generate the CO-RE eBPF object on Linux
	sh scripts/generate-ebpf.sh
build-sensor: generate-ebpf ## Build the Linux eBPF sensor
	CGO_ENABLED=0 go build -tags ebpf -trimpath -o bin/contextslo-sensor ./cmd/contextslo-sensor
docker-build: ## Build the application image
	docker build -t contextslo:local .
docker-sensor: ## Build the eBPF sensor image
	docker build -f Dockerfile.sensor -t contextslo-sensor:local .
kind-deploy: ## Apply the base deployment to the current cluster
	kubectl apply -k deploy/kubernetes
clean: ## Remove generated artifacts
	rm -rf bin data coverage.out coverage.html internal/truthsensor/context_truth_bpfel.o
