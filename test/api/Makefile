export HKTPF=-api
include ../Makefile

all: build test

.PHONY: setup-test
setup-test: cleanup-test
	docker run -d \
		--name harikube_middleware_harikube_api \
		--net=host \
		$(REPO)/$(IMAGE_NAME):$(TAG) \
		--metrics-bind-address=0 \
		--listen-address=0.0.0.0:23692 --endpoint='sqlite:///db/main.db?_journal_mode=WAL&_busy_timeout=30000&_synchronous=NORMAL&_txlock=immediate&_stmt_cache_size=20&cache=shared'

	kind create cluster --name $(KIND_CLUSTER) --config $(MAKEFILE_DIR)kind-config.yaml
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

_load:
	$(info Nothing to load)

_deploy:
	$(info Nothing to deploy)

_test:
	$(MAKE) -BC $(MAKEFILE_DIR)suites e2e

_cleanup-test:
	@docker rm -f harikube_middleware_harikube_api ||:
	