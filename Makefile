REGISTRY ?= ghcr.io
REPO ?= qinqon/github-issue-resolver
TAG ?= latest
IMAGE = $(REGISTRY)/$(REPO):$(TAG)
CONTAINER_CMD ?= podman

.PHONY: build test lint image push clean

build:
	go build -o ai-agent ./cmd/ai-agent

test:
	go test ./...

lint:
	go vet ./...

image:
	$(CONTAINER_CMD) build -t $(IMAGE) -f Containerfile .

push: image
	$(CONTAINER_CMD) push $(IMAGE)

clean:
	rm -f ai-agent
