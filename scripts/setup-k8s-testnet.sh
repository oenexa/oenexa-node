#!/usr/bin/env bash
set -e

echo "==> Building Oenexa Docker Image..."
docker build -t oenexa-node:latest .

echo "==> Generating 4 determinisitc keys for PBFT..."
go run scripts/generate_testnet_keys.go 4 deploy/k8s/testnet-keys

echo "==> Creating Kubernetes Namespace..."
kubectl create namespace oenexa-testnet || true

echo "==> Creating Kubernetes Secrets for the 4 nodes..."
for i in {0..3}; do
  kubectl create secret generic oenexa-keystore-$i \
    --namespace=oenexa-testnet \
    --from-file=keystore.json=deploy/k8s/testnet-keys/oenexa-node-$i/keystore.json \
    --dry-run=client -o yaml | kubectl apply -f -
done

echo "==> Applying ConfigMaps and StatefulSets..."
kubectl apply -f deploy/k8s/testnet.yaml

echo "==> Done! Use 'kubectl get pods -n oenexa-testnet' to check status."
