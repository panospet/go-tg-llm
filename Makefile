# container registry - if registry has a value, add a slash at the end
REGISTRY_URL ?=
REGISTRY := $(if $(REGISTRY_URL),$(REGISTRY_URL)/)

all: build

.PHONY: build
build:
	CGO_ENABLED=0 go build -ldflags='-w -s -extldflags "-static"' -o ./bin/telegram-llm-bot main.go

.PHONY: container
container: ## create docker container
	docker build -t $(REGISTRY)telegram-llm-bot .

.PHONY: container-push
container-push: ## push docker image to registry
	docker push $(REGISTRY)telegram-llm-bot