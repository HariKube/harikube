MAKEFLAGS += --no-print-directory
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

KINE_TYPE?=workload
TAG?=$(shell git describe --tags --abbrev=0)
KIND_CLUSTER?=harikube-e2e-test

all: build test

.PHONY: build
build:
	REPO=quay.io/harikube IMAGE_NAME=harikube TAG=$(TAG)-snapshot $(MAKE) -C ../.. build package harikube-release

.PHONY: setup-test
setup-test: cleanup-test
	kind create cluster --name $(KIND_CLUSTER)
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

	kind load docker-image -n $(KIND_CLUSTER) quay.io/harikube/harikube:$(TAG)-snapshot

	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

.PHONY: test
test: setup-test
	kubectl apply -f ../../package/vcluster-harikube-sqlite-$(KINE_TYPE)-$(TAG)-snapshot.yaml

	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m
	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

	vcluster connect harikube-vcluster
	$(MAKE) _test
	vcluster disconnect
	$(MAKE) cleanup-test

_test:
	chainsaw test --test-dir .

_sonobuoy:
	sonobuoy run --dns-pod-labels='k8s-app=vcluster-kube-dns' --e2e-focus='sig-api-machinery' --e2e-skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault'

.PHONY: cleanup-test
cleanup-test:
	@kind delete cluster --name $(KIND_CLUSTER)