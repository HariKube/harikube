vcluster:
	chainsaw test --test-dir infra-vcluster

simple:
	chainsaw test --test-dir simple

e2e:
	docker run --rm --net=host -i \
		-v $(KUBECONFIG):$(KUBECONFIG) \
		-e KUBECONFIG=$(KUBECONFIG) \
		--entrypoint sh \
		registry.k8s.io/conformance:v1.36.2 \
			-c "e2e.test --ginkgo.seed=1 --ginkgo.focus='api-machinery' --ginkgo.skip='StorageVersionAPI|Slow|Flaky|Alpha|OffByDefault' --ginkgo.flake-attempts=3 --ginkgo.timeout=1h -e2e-verify-service-account=false"