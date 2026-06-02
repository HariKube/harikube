MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

export TAG ?= $(shell git describe --tags --abbrev=0)-snapshot

export REPO ?= quay.io/harikube
export IMAGE_NAME ?= harikube
export KIND_CLUSTER ?= harikube-test
export KUBECONFIG ?= $(HOME)/.kube/config

export NAMESPACE = harikube

all: build test-all

.PHONY: build
build:
	$(MAKE) -C $(MAKEFILE_DIR)/.. build package harikube-release

.PHONY: test-all
test-all:
	$(MAKE) -C api test
	$(MAKE) -C e2e test
	$(MAKE) -C helm test
	$(MAKE) -C rke2 test

.PHONY: setup-test
setup-test: cleanup-test
	kind create cluster --name $(KIND_CLUSTER)
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

	$(MAKE) _setup-test

_setup-test:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl apply -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.77.1/stripped-down-crds.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

.PHONY: cleanup-test
cleanup-test: _cleanup-test
	@kind delete cluster --name $(KIND_CLUSTER)

.PHONY: test
test: setup-test _load _deploy _test
	$(MAKE) cleanup-test

.PHONY: ssh
ssh:
	docker exec -it $(KIND_CLUSTER)-control-plane bash
