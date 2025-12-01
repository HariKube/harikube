TAG?=$(shell git describe --tags --abbrev=0)
KIND_VERSION?=v0.30.0
KUBE_VERSION?=v1.34.0
VCLUSTER_VERSION?=v0.30.0
SONOBUOY_VERSION?=0.57.3

E2E_FOCUS?=sig-api-machinery
E2E_SKIP?=Alpha|Flaky|[Aa]uto[Ss]cal|mysql|zookeeper|redis|CockroachDB|ClusterTrustBundle|SchedulerAsyncPreemption|BoundServiceAccountTokenVolume|StorageVersionAPI|StatefulUpgrade|CoordinatedLeaderElection|capture the life of a ResourceClaim|should rollback without unnecessary restarts|should grab all metrics from|compacted away

KIND?=./bin/kind
KUBECTL?=./bin/kubectl
VCLUSTER?=./bin/vcluster
SONOBUOY?=./bin/sonobuoy
DOCKER?=docker

all: deps build start test

deps:
	rm -rf bin ; mkdir bin

	curl -Lo $(KIND) https://kind.sigs.k8s.io/dl/$(KIND_VERSION)/kind-linux-amd64
	chmod +x $(KIND)

	curl -Lo $(KUBECTL) https://dl.k8s.io/release/$(KUBE_VERSION)/bin/linux/amd64/kubectl
	chmod +x $(KUBECTL)

	curl -Lo $(VCLUSTER) https://github.com/loft-sh/vcluster/releases/download/$(VCLUSTER_VERSION)/vcluster-linux-amd64
	chmod +x $(VCLUSTER)

	cd bin ; curl -L https://github.com/vmware-tanzu/sonobuoy/releases/download/v$(SONOBUOY_VERSION)/sonobuoy_$(SONOBUOY_VERSION)_linux_amd64.tar.gz | tar xvz sonobuoy
	chmod +x $(SONOBUOY)

build:
	TAG=$(TAG) $(MAKE) -C ../.. build package kine-release
	$(DOCKER) tag docker.io/rancher/kine:$(TAG) quay.io/harikube/kine:$(TAG)

start: start-cluster start-vcluster

stop: stop-cluster

start-cluster: stop-cluster
	$(KIND) create cluster --name kine-cluster --config kind-configs/config-$(KUBE_VERSION).yaml
	$(KUBECTL) wait --for=condition=Ready node/kine-cluster-control-plane --timeout=120s

stop-cluster:
	$(KIND) delete cluster -n kine-cluster ||:

start-vcluster: stop-vcluster build
	$(KIND) load docker-image -n kine-cluster quay.io/harikube/kine:$(TAG)

	$(KUBECTL) create namespace kine
	
	$(KUBECTL) apply -f ../../package/vcluster-kine-sqlite-$(TAG).yaml
	$(KUBECTL) wait -n kine --for=jsonpath='{.status.readyReplicas}'=1 deploy/kine --timeout=90s
	$(KUBECTL) wait -n kine --for=jsonpath='{.status.readyReplicas}'=1 statefulset/kine --timeout=240s

	$(VCLUSTER) connect kine

stop-vcluster:
	$(VCLUSTER) disconnect ||:
	$(KUBECTL) delete namespace kine ||:

test:
	$(SONOBUOY) run --e2e-focus="$(E2E_FOCUS)" --e2e-skip="$(E2E_SKIP)"
	$(KUBECTL) wait -n sonobuoy --for=condition=Ready pod/sonobuoy --timeout=60s

logs:
	$(SONOBUOY) logs -f
