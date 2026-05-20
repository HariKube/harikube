MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

KINE_TYPE?=workload
TAG?=$(shell git describe --tags --abbrev=0)
KIND_CLUSTER?=harikube-helm-chart-test

all: build test

.PHONY: build
build:
	REPO=quay.io/harikube IMAGE_NAME=harikube TAG=$(TAG)-snapshot $(MAKE) -C ../.. build package harikube-release

.PHONY: setup
setup: cleanup
	kind create cluster --name $(KIND_CLUSTER)
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

	kind load docker-image -n $(KIND_CLUSTER) quay.io/harikube/harikube:$(TAG)-snapshot

	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

.PHONY: test
test: setup
	kubectl apply -f ../../package/vcluster-harikube-sqlite-$(KINE_TYPE)-$(TAG)-snapshot.yaml

	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m
	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

	vcluster connect harikube-vcluster
	$(MAKE) _test
	vcluster disconnect
	$(MAKE) cleanup

_test:
	chainsaw test --test-dir 00-shirt

.PHONY: cleanup
cleanup:
	@kind delete cluster --name $(KIND_CLUSTER)