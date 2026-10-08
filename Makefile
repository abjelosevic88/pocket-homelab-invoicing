VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= ghcr.io/abjelosevic88/pocket-homelab-invoicing
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: all web build run dev test lint docker docker-push clean backup

all: web build

web:                       ## build the React UI into web/dist
	cd web && npm ci --no-audit --no-fund && npm run build

build:                     ## build the Go binary (embeds web/dist)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o pocket-invoicing ./cmd/pocket-invoicing

run: build                 ## run locally with demo data
	DATA_DIR=./data DEMO_DATA=true LOG_LEVEL=debug ./pocket-invoicing

dev:                       ## backend on :8080 + vite dev server on :5173 with hot reload
	@echo "Starting API on :8080 and Vite on :5173 (open http://localhost:5173)"
	@(DATA_DIR=./data DEMO_DATA=true LOG_LEVEL=debug go run ./cmd/pocket-invoicing &) && cd web && npm run dev

test:                      ## run Go tests and type-check the UI
	go vet ./... && go test ./...
	cd web && npx tsc --noEmit

lint:
	gofmt -l . && go vet ./...

docker:                    ## build the container image for the local platform
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

docker-multiarch:          ## build & push amd64 + arm64 (needs buildx)
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest --push .

backup:                    ## snapshot the local database
	./pocket-invoicing backup

clean:
	rm -rf pocket-invoicing web/dist web/node_modules dist

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
