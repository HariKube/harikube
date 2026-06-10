
MAKEFLAGS += --no-print-directory

all: lint

.PHONY: lint
lint:
	helm lint ./harikube
	yamllint --strict --format github <(make render)
	helm template harikube ./harikube | kubeconform -summary -verbose -ignore-missing-schemas

.PHONY: render
render:
	@helm template harikube ./harikube


