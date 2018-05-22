#!/bin/sh
kubectl create -f ./rbac.yaml && kubectl create -f ./deployments.yaml
