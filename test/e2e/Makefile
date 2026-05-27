include ../Makefile

all: build test

_deploy:
	kind load docker-image -n $(KIND_CLUSTER) $(REPO)/$(IMAGE_NAME):$(TAG)

	kubectl apply -f ../../package/vcluster-harikube-sqlite-$(KINE_TYPE)-$(TAG).yaml

	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m
	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

	vcluster connect harikube-vcluster

_test:
	chainsaw test --test-dir .

_sonobuoy:
	sonobuoy run --dns-pod-labels='k8s-app=vcluster-kube-dns' --e2e-focus='sig-api-machinery' --e2e-skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault'

_cleanup-test:
	@vcluster disconnect ||:
