MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

REPO?=quay.io/harikube
IMAGE_NAME?=harikube
TAG?=$(shell git describe --tags --abbrev=0)
KIND_CLUSTER?=harikube-api-test
KUBECONFIG?=~/.kubernetes/config

all: build test

.PHONY: build
build:
	REPO=$(REPO) IMAGE_NAME=$(IMAGE_NAME) TAG=$(TAG)-snapshot $(MAKE) -C ../.. build package

.PHONY: setup-test
setup-test: cleanup-test
	docker run -d \
		--name harikube_middleware \
		--net=host \
		$(REPO)/$(IMAGE_NAME):$(TAG)-snapshot \
		--listen-address=0.0.0.0:2369 --endpoint='sqlite:///db/main.db?_journal=WAL&cache=shared'

	kind create cluster --name $(KIND_CLUSTER) --config kind-config.yaml
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

.PHONY: test
test: setup-test _test
	$(MAKE) cleanup-test

_test:
	docker run --rm --net=host -it \
		-v $(KUBECONFIG):$(KUBECONFIG) \
		-e KUBECONFIG=$(KUBECONFIG) \
		--entrypoint sh \
		registry.k8s.io/conformance:v1.36.0 \
			-c "e2e.test --ginkgo.seed=1 --ginkgo.focus='api-machinery' --ginkgo.skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault' --ginkgo.flake-attempts=3 --ginkgo.timeout=3h"

.PHONY: cleanup-test
cleanup-test:
	@kind delete cluster --name $(KIND_CLUSTER)
	@docker rm -f harikube_middleware ||: