include ../Makefile

all: build test

_deploy:
	kubectl create namespace $(NAMESPACE)
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-middleware=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-apiserver=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-controllermanager=enabled --overwrite
	kubectl label namespace $(NAMESPACE) harikube.info/$(NAMESPACE)-vcluster=enabled --overwrite

	helm install harikube ../../harikube-helm-charts/harikube \
		--debug \
		--namespace $(NAMESPACE) \
		--set middleware.networkPolicy.create=true \
		--set apiServer.networkPolicy.create=true \
		--set controllerManager.networkPolicy.create=true \
		--set vcluster.networkPolicy.create=true \
		--set middleware.monitoring.create=true \
		--set apiServer.monitoring.create=true \
		--set controllerManager.monitoring.create=true
	kubectl wait -n $(NAMESPACE) --for=jsonpath='{.status.readyReplicas}'=1 deployment/harikube-middleware-deploy --timeout=2m

	helm install harikube-vcluster https://charts.loft.sh/charts/vcluster-0.32.1.tgz \
		--debug \
		--namespace $(NAMESPACE) \
		--values harikube/vcluster/$(KINE_TYPE)-config.yaml
	kubectl wait -n $(NAMESPACE) --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube-vcluster --timeout=5m

	helm install harikube-control-plane ../../harikube-helm-charts/harikube \
		--debug \
		--namespace $(NAMESPACE) \
		--set middleware.create=false \
		--set apiServer.create=true \
		--set controllerManager.create=true

	vcluster connect harikube-vcluster

_test:
	chainsaw test --test-dir ../e2e

_cleanup-test:
	@vcluster disconnect ||:
