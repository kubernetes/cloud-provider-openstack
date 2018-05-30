#!/bin/sh
kubectl create -f ./sc.yaml && kubectl create -f ./pvc.yaml
