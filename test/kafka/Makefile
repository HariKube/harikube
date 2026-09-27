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

	cat $(MAKEFILE_DIR)kafka/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"

	cat $(MAKEFILE_DIR)kafka/shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"
	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic dlq --bootstrap-server broker:29092 --max-messages 2 --timeout-ms 100000

_cleanup-test:
	@(cd $(MAKEFILE_DIR)kafka ; docker compose down) ||:
	@docker rm -f harikube_middleware_harikube_kafka ||:
	