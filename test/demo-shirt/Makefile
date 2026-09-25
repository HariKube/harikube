include ../kafka/Makefile
export HKTPF=-demo-shirt

_deploy:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
	kubectl wait -n cert-manager --for=jsonpath='{.status.readyReplicas}'=1 deployment/cert-manager-webhook --timeout=2m
	sleep 5

	kubectl apply -f $(MAKEFILE_DIR)demo-shirt/api-extension-config.yaml
	kubectl apply -f https://github.com/HariKube/api-extension/releases/download/beta-v1.0.0-1/bundle-beta-v1.0.0-1.yaml
	kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 deployment/api-extension-controller-manager --timeout=2m

	kubectl apply -f https://github.com/HariKube/serverless-kube-watch-trigger/releases/download/beta-v1.0.0-10/bundle-beta-v1.0.0-10.yaml
	kubectl wait -n serverless-kube-watch-trigger-system --for=jsonpath='{.status.readyReplicas}'=1 deployment/serverless-kube-watch-trigger-controller-manager --timeout=2m


_test:
	$(MAKE) -BC $(MAKEFILE_DIR)suites simple

	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic outgoing --bootstrap-server broker:29092 --max-messages 5 --timeout-ms 1000

	cat $(MAKEFILE_DIR)demo-shirt/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"
	kubectl wait --for=create shirts/coming-on-kafka-stream --timeout=1m

	cat $(MAKEFILE_DIR)demo-shirt/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"
	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic dlq --bootstrap-server broker:29092 --max-messages 1 --timeout-ms 100000

	kubectl delete shirts coming-on-kafka-stream
	