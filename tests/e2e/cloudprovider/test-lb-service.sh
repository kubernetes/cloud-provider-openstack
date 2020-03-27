#!/usr/bin/env bash

# This script is used for the openlab CI job cloud-provider-openstack-acceptance-test-lb-octavia.
# See https://github.com/theopenlab/openlab-zuul-jobs/blob/master/playbooks/cloud-provider-openstack-acceptance-test-lb-octavia/run.yaml#L104
#
# Prerequisites:
#   - This script is supposed to be running on a host has access to kubernetes cluster.
#   - kubectl is ready to talk with the kubernetes cluster.
#   - It's recommended to run the script on a host with as less proxies to the public as possible, otherwise the
#     x-forwarded-for test will probably fail.
#   - This script is not responsible for resource clean up if there is test case fails.

TIMEOUT=${TIMEOUT:-300}
FLOATING_IP=${FLOATING_IP:-""}
NAMESPACE="octavia-lb-test"

########################################################################
## Name: wait_for_service
## Desc: Waits for a k8s service until it gets a valid IP address
## Params:
##   - (required) A k8s service name
########################################################################
function wait_for_service {
  local service_name=$1

  end=$(($(date +%s) + ${TIMEOUT}))
  while true; do
    ipaddr=$(kubectl -n $NAMESPACE describe service ${service_name} | grep 'LoadBalancer Ingress' | awk -F":" '{print $2}' | tr -d ' ')
    if [ "x${ipaddr}" != "x" ]; then
      printf "\n>>>>>>> Service ${service_name} is created successfully, IP: ${ipaddr}\n"
      export ipaddr=${ipaddr}
      break
    fi
    sleep 3
    now=$(date +%s)
    [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Failed to wait for the Service ${service_name} created in time\n" && exit -1
  done
}

########################################################################
## Name: wait_for_service_deleted
## Desc: Waits for a k8s service deleted.
## Params:
##   - (required) A k8s service name
########################################################################
function wait_for_service_deleted {
    local service_name=$1

    end=$(($(date +%s) + ${TIMEOUT}))
    while true; do
        svc=$(kubectl -n $NAMESPACE get service | grep ${service_name})
        if [[ "x${svc}" == "x" ]]; then
            printf "\n>>>>>>> Service ${service_name} deleted\n"
            break
        fi
        sleep 3
        now=$(date +%s)
        [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Failed to wait for the Service ${service_name} deleted\n" && exit -1
    done
}

########################################################################
## Name: create_namespace
## Desc: Makes sure the namespace exists.
## Params: None
########################################################################
function create_namespace {
    printf "\n>>>>>>> Create Namespace ${NAMESPACE}\n"
    kubectl -n $NAMESPACE get deploy echoserver > /dev/null 2>&1
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
EOF
}

########################################################################
## Name: create_deployment
## Desc: Makes sure the echoserver service Deployment is running
## Params: None
########################################################################
function create_deployment {
    printf "\n>>>>>>> Create a Deployment\n"
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echoserver
  namespace: $NAMESPACE
  labels:
    run: echoserver
spec:
  replicas: 1
  selector:
    matchLabels:
      run: echoserver
  template:
    metadata:
      labels:
        run: echoserver
    spec:
      containers:
      - name: echoserver
        image: gcr.io/google-containers/echoserver:1.10
        imagePullPolicy: IfNotPresent
        ports:
          - containerPort: 8080
EOF
}

########################################################################
## Name: test_basic
## Desc: Create a k8s service and send request to the service external
##       IP
## Params: None
########################################################################
function test_basic {
    local service="test-basic"

    printf "\n>>>>>>> Create Service ${service}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service}
  namespace: $NAMESPACE
spec:
  type: LoadBalancer
  loadBalancerIP: ${FLOATING_IP}
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service ${service} creation finished\n"
    wait_for_service ${service}

    printf "\n>>>>>>> Sending request to the Service ${service}\n"
    podname=$(curl -s http://${ipaddr} | grep Hostname | awk -F':' '{print $2}' | cut -d ' ' -f2)
    if [[ "$podname" =~ "echoserver" ]]; then
        printf "\n>>>>>>> Expected: Get correct response from Service ${service}\n"
    else
        printf "\n>>>>>>> FAIL: Get incorrect response from Service ${service}, expected: echoserver, actual: $podname\n"
        exit -1
    fi

    printf "\n>>>>>>> Delete Service ${service}\n"
    kubectl -n $NAMESPACE delete service ${service}
}

########################################################################
## Name: test_forwarded
## Desc: Create a k8s service that gets the original client IP
## Params: None
########################################################################
function test_forwarded {
    local service="test-x-forwarded-for"
    local public_ip=$(curl -s ifconfig.me)
    local local_ip=$(ip route get 8.8.8.8 | head -1 | awk '{print $7}')

    printf "\n>>>>>>> Create the Service ${service}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/x-forwarded-for: "true"
spec:
  type: LoadBalancer
  loadBalancerIP: ${FLOATING_IP}
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service ${service} creation finished\n"
    wait_for_service ${service}

    printf "\n>>>>>>> Sending request to the Service ${service}\n"
    orig_ip=$(curl -s http://${ipaddr} | grep  x-forwarded-for | awk -F'=' '{print $2}')
    if [[ "${orig_ip}" != "${local_ip}" && "${orig_ip}" != "${public_ip}" ]]; then
        printf "\n>>>>>>> FAIL: Get incorrect response from Service ${service}, orig_ip: ${local_ip}, public_ip: ${public_ip}\n"
        exit -1
    else
        printf "\n>>>>>>> Expected: Get correct response from Service ${service}\n"
    fi

    printf "\n>>>>>>> Delete Service ${service}\n"
    kubectl -n $NAMESPACE delete service ${service}
}

create_namespace
create_deployment
test_basic
test_forwarded

printf "\n>>>>>>> Delete k8s resources\n"
kubectl -n ${NAMESPACE} delete deploy echoserver