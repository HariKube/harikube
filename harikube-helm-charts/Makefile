
MAKEFLAGS += --no-print-directory

all: lint

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


