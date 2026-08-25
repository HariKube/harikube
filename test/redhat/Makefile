include ../Makefile

DAGGER_NO_NAG=1
export DAGGER_NO_NAG

all: build test

.PHONY: build
build:
	$(info Nothing to build)

.PHONY: setup-test
setup-test: cleanup-test
	$(info Nothing to setup)

_load:
	$(info Nothing to load)

_deploy:
	$(info Nothing to deploy)

_test: _testCharts _testContainers

_testContainers:
	@rm -rf $(MAKEFILE_DIR)redhat/report-containers

	dagger call check-containers export --path $(MAKEFILE_DIR)redhat/report-containers

_testCharts:
	@rm -rf $(MAKEFILE_DIR)redhat/report-charts

	dagger call check-charts export --path $(MAKEFILE_DIR)redhat/report-charts

_cleanup-test:
	dagger core engine local-cache prune --max-used-space 0 --use-default-policy

.PHONY: ssh
ssh:
	$(info No host to SSH)