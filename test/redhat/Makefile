include ../Makefile

DAGGER_NO_NAG=1
export DAGGER_NO_NAG

all: build test

.PHONY: build
build:

.PHONY: setup-test
setup-test: cleanup-test

_load:

_deploy:

_test: _testCharts _testContainers

_testContainers:
	@rm -rf $(MAKEFILE_DIR)openshift/report-containers

	dagger call check-containers export --path $(MAKEFILE_DIR)openshift/report-containers

_testCharts:
	@rm -rf $(MAKEFILE_DIR)openshift/report-charts

	dagger call check-charts export --path $(MAKEFILE_DIR)openshift/report-charts

.PHONY: cleanup-test
cleanup-test:
	
_cleanup-test:

.PHONY: ssh
ssh:
