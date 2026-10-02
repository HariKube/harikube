include ../kafka/Makefile
export HKTPF=-demo-todo

WORKER_KUBECONFIG ?=

_deploy:
	kubectl create secret generic pi-agent-config \
		--from-file=$(HOME)/.pi/agent/auth.json \
		--from-file=$(HOME)/.pi/agent/models-store.json \
		--from-file=models.json=/dev/null \
		--from-file=$(HOME)/.pi/agent/settings.json \
		--dry-run=client -o yaml | kubectl apply -f -

	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

	kubectl apply -f https://github.com/HariKube/api-extension/releases/download/rc-v1.0.0-2/bundle-config-rc-v1.0.0-2.yaml
	kubectl apply -f https://github.com/HariKube/api-extension/releases/download/rc-v1.0.0-2/bundle-rc-v1.0.0-2.yaml
	kubectl apply -f https://github.com/HariKube/serverless-kube-watch-trigger/releases/download/rc-v1.0.0-8/bundle-rc-v1.0.0-8.yaml

	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/api-extension-controller-manager --timeout=2m
	kubectl wait -n serverless-kube-watch-trigger-system --for=jsonpath='{.status.readyReplicas}'=1 deployment/serverless-kube-watch-trigger-controller-manager --timeout=2m

_test:
	chainsaw test --test-dir interactive
