#!/bin/bash

POD_NAME=$(kubectl get pods -l app=csi-nodeplugin-manilaplugin -o=name | head -n 1)

function get_pod_status() {
	echo -n $(kubectl get $POD_NAME -o jsonpath="{.status.phase}")
}

while [[ $(get_pod_status) != "Running" ]]; do
	sleep 1
	echo "Waiting for $POD_NAME (status $(get_pod_status))"
done


kubectl logs -f $POD_NAME -c csi-nodeplugin-manilaplugin
