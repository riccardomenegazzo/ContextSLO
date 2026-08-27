.DEFAULT_GOAL := help

.PHONY: help run test test-race build fmt vet docker-build kind-deploy clean
help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; printf "ContextSLO developer commands\n\n"} /^[a-zA-Z_-]+:.*?## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: ## Run the dashboard on localhost:8080
	go run ./cmd/contextslo serve --data ./data/state.json

test: ## Run unit and API tests
	go test ./...

test-race: ## Run tests with the race detector
	go test -race ./...

build: ## Build the CLI and server
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/contextslo ./cmd/contextslo

fmt: ## Format Go source
	gofmt -w cmd internal

vet: ## Run Go static analysis
	go vet ./...

docker-build: ## Build the container image
	docker build -t contextslo:local .

kind-deploy: ## Deploy the demo to the current Kubernetes context
	kubectl apply -k deploy/kubernetes

clean: ## Remove generated local artifacts
	rm -rf bin data coverage.out coverage.html
