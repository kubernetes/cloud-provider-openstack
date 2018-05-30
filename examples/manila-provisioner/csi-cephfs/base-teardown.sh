#!/bin/sh
kubectl delete -f ./deployments.yaml
kubectl delete -f ./daemonsets.yaml
kubectl delete -f ./statefulsets.yaml
kubectl delete -f ./services.yaml
kubectl delete -f ./rbac.yaml
