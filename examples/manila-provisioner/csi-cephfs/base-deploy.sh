#!/bin/sh
kubectl create -f ./rbac.yaml && kubectl create -f ./services.yaml && kubectl create -f ./statefulsets.yaml && kubectl create -f ./daemonsets.yaml && kubectl create -f ./deployments.yaml
