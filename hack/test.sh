#! /bin/bash -e

REPO=harikube make build package

TAG=dev make harikube-release

kind create cluster

kind load docker-container 

kubectl apply -f package/vcluster-harikube-sqlite-dev.yaml

kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube --timeout=5m

vcluster connect harikube

kubectl apply -f hack/vcluster/skip-controller-manager-metadata-caching.yaml

kubectl apply -f https://raw.githubusercontent.com/kubernetes/website/main/content/en/examples/customresourcedefinition/shirt-resource-definition.yaml

cat | kubectl apply -f - <<EOF
apiVersion: stable.example.com/v1
kind: Shirt
metadata:
  name: example1
  labels:
    color: blue
spec:
  color: blue
  size: S
EOF
cat | kubectl apply -f - <<EOF
apiVersion: stable.example.com/v1
kind: Shirt
metadata:
  name: example2
  labels:
    color: blue
  ownerReferences:
  - apiVersion: stable.example.com/v1
    kind: Shirt
    name: example1
    uid: $(kubectl get shirts example1 -o jsonpath='{.metadata.uid}')
spec:
  color: blue
  size: M
---
apiVersion: stable.example.com/v1
kind: Shirt
metadata:
  name: example3
  labels:
    color: green
  finalizers:
  - harikube.info/block
spec:
  color: green
  size: M
EOF

# kind delete cluster