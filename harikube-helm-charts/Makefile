
MAKEFLAGS += --no-print-directory

NAMESPACE = harikube
KINE_TYPE?=workload
TAG?=$(shell git describe --tags --abbrev=0)
KIND_CLUSTER ?= harikube-helm-chart-test

all: lint build test

.PHONY: lint
lint:
	helm lint ./harikube
	yamllint --strict --format github <(make render)
	helm template harikube ./harikube | kubeconform -summary -verbose -ignore-missing-schemas
	@helm template harikube ./harikube \
		--set middleware.create=false \
		--set apiServer.create=false \
		--set controllerManager.create=false \
		| grep ":" && exit 1 || echo ok

.PHONY: render
render:
	@helm template harikube ./harikube

.PHONY: build
build:
	REPO=quay.io/harikube IMAGE_NAME=harikube TAG=$(TAG)-snapshot $(MAKE) -C .. build package harikube-release

.PHONY: setup
setup: cleanup
	kind create cluster --name $(KIND_CLUSTER)
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

	kind load docker-image -n $(KIND_CLUSTER) quay.io/harikube/harikube:$(TAG)-snapshot

	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl apply -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.77.1/stripped-down-crds.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

.PHONY: test
test: setup _setup _test
	$(MAKE) cleanup

_setup:
	kubectl create namespace $(NAMESPACE)
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-middleware=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-apiserver=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-controllermanager=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-vcluster=enabled --overwrite

	helm install harikube ./harikube \
		--debug \
		--namespace $(NAMESPACE) \
		--set middleware.networkPolicy.create=true \
		--set apiServer.networkPolicy.create=true \
		--set controllerManager.networkPolicy.create=true \
		--set vcluster.networkPolicy.create=true \
		--set middleware.monitoring.create=true \
		--set apiServer.monitoring.create=true \
		--set controllerManager.monitoring.create=true
	kubectl wait -n $(NAMESPACE) --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m

	helm install harikube-vcluster https://charts.loft.sh/charts/vcluster-0.32.1.tgz \
		--debug \
		--namespace $(NAMESPACE) \
		--values harikube/vcluster/$(KINE_TYPE)-config.yaml
	kubectl wait -n $(NAMESPACE) --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

	helm install harikube-control-plane ./harikube \
		--debug \
		--namespace $(NAMESPACE) \
		--set middleware.create=false \
		--set apiServer.create=true \
		--set controllerManager.create=true

_test:
	vcluster connect harikube-vcluster
	chainsaw test --test-dir ../test/e2e/00-shirt
	vcluster disconnect

.PHONY: cleanup
cleanup:
	@kind delete cluster --name $(KIND_CLUSTER)
