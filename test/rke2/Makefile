include ../Makefile

all: build test
	
.PHONY: setup-test
setup-test: cleanup-test
	vagrant up --provider=libvirt

_load:
	docker image save $(REPO)/$(IMAGE_NAME):$(TAG) -o $(TAG).tar
	vagrant rsync
	vagrant ssh -c "sudo docker load -i /vagrant/$(TAG).tar"
	vagrant ssh -c "sudo docker tag $(REPO)/$(IMAGE_NAME):$(TAG) harikube:target"

_deploy:
	vagrant ssh -c "sudo systemctl enable harikube.service --now"

	vagrant ssh -c "sudo sh -c 'curl -sfL https://get.rke2.io | INSTALL_RKE2_VERSION=v1.36.0+rke2r1 sh -'"
	vagrant ssh -c "sudo systemctl enable rke2-server.service --now"

	@rm -f $(KUBECONFIG) ; vagrant ssh -c "sudo cat /etc/rancher/rke2/rke2.yaml" > $(KUBECONFIG)
	@sed -i "s/127.0.0.1/$$(vagrant ssh-config | grep HostName | awk '{print $$2}')/" $(KUBECONFIG)

	kubectl wait --for=condition=Ready node/harikube-rke2 --timeout=120s

	$(MAKE) _setup-test

	kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/master/deploy/local-path-storage.yaml
	kubectl wait -n local-path-storage --for=jsonpath='{.status.readyReplicas}'=1 deployment/local-path-provisioner --timeout=2m
	kubectl apply -f storageclass.yaml

	vagrant ssh -c "sudo /var/lib/rancher/rke2/bin/ctr --address /run/k3s/containerd/containerd.sock --namespace k8s.io images import /vagrant/$(TAG).tar"
	$(MAKE) -C ../helm _deploy

_test: _test-host _test-vcluster

_test-host:
	$(MAKE) -C ../api _test

_test-vcluster:
	vcluster connect harikube-vcluster
	$(MAKE) -C ../api _test
	vcluster disconnect ||:

.PHONY: cleanup-test
cleanup-test:
	@vagrant destroy -f ||:
	
_cleanup-test:

ssh:
	vagrant ssh -c "sudo su"
