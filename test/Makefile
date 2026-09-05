MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

TAG ?= $(shell git describe --tags --abbrev=0)-snapshot
export TAG

REPO ?= quay.io/harikube
export REPO
IMAGE_NAME ?= harikube
export IMAGE_NAME
KIND_CLUSTER ?= harikube-test$(HKTPF)
export KIND_CLUSTER
KUBECONFIG ?= $(HOME)/.kube/config
export KUBECONFIG

export NAMESPACE = harikube

all: build test-all

.PHONY: build
build:
	$(MAKE) -BC $(MAKEFILE_DIR).. build package harikube-release

.PHONY: test-all
test-all:
	$(MAKE) -BC api test
	$(MAKE) -BC helm test
	$(MAKE) -BC openshift test
	$(MAKE) -BC redhat test
	$(MAKE) -BC rke2 test

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
