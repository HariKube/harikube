export HKTPF=-kafka
include ../Makefile

all: build test

.PHONY: setup-test
setup-test: cleanup-test
	(cd $(MAKEFILE_DIR)kafka ; docker compose up -d)

	sleep 5
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic incoming --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic dlq --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic outgoing --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1

	docker run -d \
		--name harikube_middleware_harikube_kafka \
		--net=host \
		-e KAFKA_INGRESS="$$(base64 -w0 $(MAKEFILE_DIR)kafka/kafka-ingress.json)" \
		-e KAFKA_INGRESS_DLQ="$$(base64 -w0 $(MAKEFILE_DIR)kafka/kafka-ingress-dlq.json)" \
		-e KAFKA_ENGRESS="$$(base64 -w0 $(MAKEFILE_DIR)kafka/kafka-engress.json)" \
		-e KAFKA_ENGRESS_DLQ="$$(base64 -w0 $(MAKEFILE_DIR)kafka/kafka-ingress-dlq.json)" \
		-e KUBECONFIG=/etc/kubeconfig \
		-v $(KUBECONFIG):/etc/kubeconfig \
		-u 0:0 \
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
	$(MAKE) -BC $(MAKEFILE_DIR)suites simple

	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic outgoing --bootstrap-server broker:29092 --max-messages 5 --timeout-ms 1000

	# Produce Shirt create (existing flow)
	cat $(MAKEFILE_DIR)kafka/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"

	# Produce ConfigMap create (inline payload)
	printf '%s' '/registry/configmaps/default/cm-from-kafka#create:{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-from-kafka","namespace":"default"},"data":{"k":"v"}}' | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"

	# Produce ClusterRole create (inline payload)
	printf '%s' '/registry/clusterroles/cr-from-kafka#create:{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"cr-from-kafka"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}' | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"

	# Wait for all three resources to be created
	kubectl wait --for=create shirts/coming-on-kafka-stream --timeout=1m
	kubectl wait --for=create configmap/cm-from-kafka -n default --timeout=1m
	kubectl wait --for=create clusterrole/cr-from-kafka --timeout=1m

	# Send duplicate Shirt to exercise DLQ (retain existing duplicate behavior)
	cat $(MAKEFILE_DIR)kafka/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"

	# Expect one message on DLQ (the duplicate Shirt)
	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic dlq --bootstrap-server broker:29092 --max-messages 1 --timeout-ms 100000

	# Cleanup created resources (bounded)
	kubectl delete shirts coming-on-kafka-stream ||:
	kubectl delete configmap cm-from-kafka -n default ||:
	kubectl delete clusterrole cr-from-kafka ||:

_cleanup-test:
	@(cd $(MAKEFILE_DIR)kafka ; docker compose down) ||:
	@docker rm -f harikube_middleware_harikube_kafka ||:
	