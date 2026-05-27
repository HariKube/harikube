MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

TAG?=$(shell git describe --tags --abbrev=0)-snapshot

REPO?=quay.io/harikube
IMAGE_NAME?=harikube
KIND_CLUSTER?=harikube-test
KUBECONFIG?=$(HOME)/.kube/config
KIND_CLUSTER ?= harikube-test

NAMESPACE=harikube

all: build test-all

.PHONY: build
build:
	REPO=$(REPO) IMAGE_NAME=$(IMAGE_NAME) TAG=$(TAG) $(MAKE) -C $(MAKEFILE_DIR)/.. build package harikube-release

.PHONY: test-all
test-all:
	$(MAKE) -C api test
	$(MAKE) -C e2e test
	$(MAKE) -C helm test

.PHONY: setup-test
setup-test: cleanup-test
	kind create cluster --name $(KIND_CLUSTER)
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl apply -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.77.1/stripped-down-crds.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

.PHONY: cleanup-test
cleanup-test: _cleanup-test
	@kind delete cluster --name $(KIND_CLUSTER)

.PHONY: test
test: setup-test _deploy _test
	$(MAKE) cleanup-test
