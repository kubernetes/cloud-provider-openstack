#!/bin/sh
kubectl delete -f ./deployments.yaml
kubectl delete -f ./rbac.yaml
