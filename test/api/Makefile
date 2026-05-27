include ../Makefile

all: build test

.PHONY: setup-test
setup-test: cleanup-test
	docker run -d \
		--name harikube_middleware \
		--net=host \
		$(REPO)/$(IMAGE_NAME):$(TAG) \
		--listen-address=0.0.0.0:2369 --endpoint='sqlite:///db/main.db?_journal=WAL&cache=shared'

	kind create cluster --name $(KIND_CLUSTER) --config kind-config.yaml
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

_deploy:

_test:
	docker run --rm --net=host -i \
		-v $(KUBECONFIG):$(KUBECONFIG) \
		-e KUBECONFIG=$(KUBECONFIG) \
		--entrypoint sh \
		registry.k8s.io/conformance:v1.36.0 \
			-c "e2e.test --ginkgo.seed=1 --ginkgo.focus='api-machinery' --ginkgo.skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault' --ginkgo.flake-attempts=3 --ginkgo.timeout=3h"

_cleanup-test:
	@docker rm -f harikube_middleware ||:
	