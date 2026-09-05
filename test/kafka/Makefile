export HKTPF=-kafka
include ../Makefile

all: build test

.PHONY: setup-test
setup-test: cleanup-test
	docker compose up -d

	sleep 5
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic incoming --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic dlq --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1
	docker exec broker /opt/kafka/bin/kafka-topics.sh --create --topic outgoing --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1

	docker run -d \
		--name harikube_middleware \
		--net=host \
		-e KAFKA_INGRESS="$$(base64 -w0 kafka-ingress.json)" \
		-e KAFKA_INGRESS_DLQ="$$(base64 -w0 kafka-ingress-dlq.json)" \
		-e KAFKA_ENGRESS="$$(base64 -w0 kafka-engress.json)" \
		-e KAFKA_ENGRESS_DLQ="$$(base64 -w0 kafka-ingress-dlq.json)" \
		$(REPO)/$(IMAGE_NAME):$(TAG) \
		--listen-address=0.0.0.0:2369 --endpoint='sqlite:///db/main.db?_journal=WAL&cache=shared'

	kind create cluster --name $(KIND_CLUSTER) --config kind-config.yaml
	kubectl wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=240s

_load:
	$(info Nothing to load)

_deploy:
	$(info Nothing to deploy)

_test:
	$(MAKE) -BC ../suites simple

	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic outgoing --bootstrap-server broker:29092 --max-messages 5 --timeout-ms 1000

	cat shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"
	kubectl wait --for=create shirts/coming-on-kafka-stream --timeout=1m

	cat shirt-example.msg | docker exec -i -w /opt/kafka/bin broker ./kafka-console-producer.sh --topic incoming --bootstrap-server broker:29092 --reader-property "parse.key=true" --reader-property "key.separator=:"
	docker exec -w /opt/kafka/bin broker ./kafka-console-consumer.sh --from-beginning --topic dlq --bootstrap-server broker:29092 --max-messages 1 --timeout-ms 100000

	kubectl delete shirts coming-on-kafka-stream

_cleanup-test:
	@docker compose down ||:
	@docker rm -f harikube_middleware ||:
	