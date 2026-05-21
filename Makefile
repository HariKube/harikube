.DEFAULT_GOAL := ci

ARCH ?= amd64
REPO ?= rancher
DEFAULT_BUILD_ARGS=--build-arg="REPO=$(REPO)" --build-arg="TAG=$(TAG)" --build-arg="ARCH=$(ARCH)" --build-arg="DIRTY=$(DIRTY)"
DIRTY := $(shell git status --porcelain --untracked-files=no)
ifneq ($(DIRTY),)
	DIRTY="-dirty"
endif

clean:
	rm -rf ./bin ./dist

.PHONY: validate
validate:
	DOCKER_BUILDKIT=1 docker build \
		$(DEFAULT_BUILD_ARGS) --build-arg="SKIP_VALIDATE=$(SKIP_VALIDATE)" \
		--target=validate -f Dockerfile .

.PHONY: build
build:
	DOCKER_BUILDKIT=1 docker build \
		$(DEFAULT_BUILD_ARGS) --build-arg="DRONE_TAG=$(DRONE_TAG)" \
		-f Dockerfile --target=binary --output=. .

.PHONY: multi-arch-build
PLATFORMS = linux/amd64,linux/arm64,linux/arm/v7,linux/riscv64
multi-arch-build:
	docker buildx build --build-arg="REPO=$(REPO)" --build-arg="TAG=$(TAG)" --build-arg="DIRTY=$(DIRTY)" --platform=$(PLATFORMS) --target=multi-arch-binary --output=type=local,dest=bin .
	mv bin/linux*/kine* bin/
	rmdir bin/linux*
	mkdir -p dist/artifacts
	cp bin/kine* dist/artifacts/

.PHONY: package
package:
	ARCH=$(ARCH) ./scripts/package

.PHONY: ci
ci: validate build package

.PHONY: test
test:
	go test -cover -tags=test $(shell go list ./... | grep -v nats)

harikube-release:
	rm -rf package ; mkdir -p package

	@cp hack/namespace.yaml package/vcluster-harikube-sqlite-api-$(TAG).yaml
	@cp hack/namespace.yaml package/vcluster-harikube-sqlite-workload-$(TAG).yaml

	@helm template harikube harikube-helm-charts/harikube \
		--namespace harikube \
		--set middleware.image.tag=$(TAG) \
		>> package/vcluster-harikube-sqlite-api-$(TAG).yaml
	@helm template harikube harikube-helm-charts/harikube \
		--namespace harikube \
		--set middleware.image.tag=$(TAG) \
		>> package/vcluster-harikube-sqlite-workload-$(TAG).yaml

	@helm template harikube-vcluster https://charts.loft.sh/charts/vcluster-0.32.1.tgz \
		--namespace harikube \
		--values harikube-helm-charts/harikube/vcluster/api-config.yaml \
		--set controlPlane.distro.k8s.image.tag=$$(grep tag harikube-helm-charts/harikube/vcluster/api-config.yaml | awk '{print $$2}') \
		>> package/vcluster-harikube-sqlite-api-$(TAG).yaml
	@helm template harikube-vcluster https://charts.loft.sh/charts/vcluster-0.32.1.tgz \
		--namespace harikube \
		--values harikube-helm-charts/harikube/vcluster/workload-config.yaml \
		--set controlPlane.distro.k8s.image.tag=$$(grep tag harikube-helm-charts/harikube/vcluster/workload-config.yaml | awk '{print $$2}') \
		>> package/vcluster-harikube-sqlite-workload-$(TAG).yaml

	@helm template harikube-helm-charts/harikube \
		--set middleware.create=false \
		--set mutatingAdmissionPolicy.create=true \
		>> package/skip-controller-manager-metadata-caching.yaml

	@helm package harikube-helm-charts/harikube \
		-d package
