.DEFAULT_GOAL := help

IMG ?= ghcr.io/pkr-lab/capulus-operator:dev

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## DeepCopy, CRDs und RBAC aus den Go-Typen erzeugen.
	go tool controller-gen object paths=./api/...
	go tool controller-gen crd rbac:roleName=capulus-operator paths=./api/... paths=./internal/... output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

.PHONY: check-generated
check-generated: generate ## Fehlschlag, wenn generierte Dateien nicht eingecheckt sind.
	git diff --exit-code -- api config

.PHONY: fmt
fmt: ## gofmt.
	gofmt -w api cmd internal

.PHONY: vet
vet: ## go vet.
	go vet ./...

.PHONY: test
test: ## Unit-Tests.
	go test ./...

.PHONY: build
build: ## Manager nach bin/manager bauen.
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/manager ./cmd/manager

.PHONY: docker-build
docker-build: ## Image bauen.
	docker build -t $(IMG) .
