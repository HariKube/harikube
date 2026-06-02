include ../Makefile

all: build test

_load:
	kind load docker-image -n $(KIND_CLUSTER) $(REPO)/$(IMAGE_NAME):$(TAG)
	
_deploy:
	kubectl apply -f ../../package/vcluster-harikube-sqlite-workload-$(TAG).yaml

	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m
	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

_test:
	vcluster connect harikube-vcluster
	chainsaw test --test-dir .
	vcluster disconnect ||:

_sonobuoy:
	vcluster connect harikube-vcluster
	sonobuoy run --dns-pod-labels='k8s-app=vcluster-kube-dns' --e2e-focus='sig-api-machinery' --e2e-skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault'
	vcluster disconnect ||:

_cleanup-test:
