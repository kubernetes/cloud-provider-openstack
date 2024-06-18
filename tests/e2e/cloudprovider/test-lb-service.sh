#!/usr/bin/env bash

# This script is used for the CI job cloud-provider-openstack-test-occm.
#
# Prerequisites:
#   - This script is supposed to be running on the CI host which is running devstack.
#   - kubectl is ready to talk with the kubernetes cluster.
#   - jq is needed.
#   - This script will delete all the resources created during testing if there is any test case fails.
set -x

TIMEOUT=${TIMEOUT:-300}
FLOATING_IP=${FLOATING_IP:-""}
NAMESPACE="octavia-lb-test"
GATEWAY_IP=${GATEWAY_IP:-""}
DEVSTACK_OS_RC=${DEVSTACK_OS_RC:-"/home/zuul/devstack/openrc"}
CLUSTER_TENANT=${CLUSTER_TENANT:-"demo"}
CLUSTER_USER=${CLUSTER_USER:-"demo"}
LB_SUBNET_NAME=${LB_SUBNET_NAME:-"private-subnet"}
AUTO_CLEAN_UP=${AUTO_CLEAN_UP:-"true"}
OCTAVIA_PROVIDER=${OCTAVIA_PROVIDER:-""}

function delete_resources() {
  ERROR_CODE="$?"

  if [[ ${AUTO_CLEAN_UP} != "true" ]]; then
    exit ${ERROR_CODE}
  fi

  printf "\n>>>>>>> Deleting k8s services\n"
  kubectl -n ${NAMESPACE} get svc -o name | xargs -r kubectl -n $NAMESPACE delete
  printf "\n>>>>>>> Deleting k8s deployments\n"
  kubectl -n ${NAMESPACE} get deploy -o name | xargs -r kubectl -n $NAMESPACE delete

  printf "\n>>>>>>> Deleting openstack load balancer \n"
  openstack loadbalancer delete test_shared_user_lb --cascade

  printf "\n>>>>>>> Deleting openstack FIPs \n"
  fips=$(openstack floating ip list --tag occm-test -f value -c ID)
  for fip in $fips; do
      openstack floating ip delete ${fip}
  done

  if [[ "$ERROR_CODE" != "0" ]]; then
    printf "\n>>>>>>> Dump openstack-cloud-controller-manager logs \n"
    pod_name=$(kubectl -n kube-system get pod -l k8s-app=openstack-cloud-controller-manager -o name | awk 'NR==1 {print}')
    kubectl -n kube-system logs ${pod_name}
  fi

  exit ${ERROR_CODE}
}
trap "delete_resources" EXIT;

function _check_lb_tags {
  local lbID=$1
  local svcName=$2
  local tags=$3

  if [ -z "$tags" ]; then
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
  fi
  if [[ ! "$tags" =~ (^|[[:space:]])kube_service_(.+?)$svcName($|[[:space:]]) ]]; then
    return 1
  fi
  return 0
}

function _check_service_lb_annotation {
  local svcName=$1

  for i in {1..3}; do
    lbID=$(kubectl -n $NAMESPACE get svc ${svcName} -o jsonpath="{.metadata.annotations.loadbalancer\.openstack\.org/load-balancer-id}")
    if [ -n "$lbID" ]; then
      echo "$lbID"
      return 0
    fi
    sleep 5
  done

  printf "\n>>>>>>> FAIL: Service annotation loadbalancer.openstack.org/load-balancer-id not found for service %s \n" $svcName
  kubectl -n $NAMESPACE get svc ${service1} -o yaml
  exit 1
}

function set_openstack_credentials {
  local XTRACE
  XTRACE=$(set +o | grep xtrace)
  set +x; source $DEVSTACK_OS_RC $CLUSTER_TENANT $CLUSTER_USER
  $XTRACE
}

########################################################################
## Name: wait_for_service
## Desc: Waits for a k8s service until it gets a valid IP address
## Params:
##   - (required) A k8s service name
########################################################################
function wait_for_service_address {
  local service_name=$1

  end=$(($(date +%s) + ${TIMEOUT}))
  while true; do
    ipaddr=$(kubectl -n $NAMESPACE describe service ${service_name} | grep 'LoadBalancer Ingress' | awk -F":" '{print $2}' | tr -d ' ')
    if [ "x${ipaddr}" != "x" ]; then
      printf "\n>>>>>>> Service ${service_name} is created successfully, IP: ${ipaddr}\n"
      export ipaddr=${ipaddr}
      break
    fi
    sleep 10
    now=$(date +%s)
    [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Timeout when waiting for the Service ${service_name} created\n" && exit 1
  done
}

########################################################################
## Name: wait_address_accessible
## Desc: Waits for the IP address accessible (port 80).
## Params:
##   - (required) An IP address
########################################################################
function wait_address_accessible {
  local addr=$1

  end=$(($(date +%s) + ${TIMEOUT}))
  while true; do
    curl -sS http://${ipaddr}
    if [ $? -eq 0 ]; then
      break
    fi
    sleep 5
    now=$(date +%s)
    [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Timeout when waiting for the IP address ${addr} accessible\n" && exit 1
  done
}

########################################################################
## Name: wait_for_loadbalancer
## Desc: Waits for the load balancer to be ACTIVE
## Params:
##   - (required) The load balancer ID.
########################################################################
function wait_for_loadbalancer {
  local lbid=$1
  local i=0
  sleep 5

  end=$(($(date +%s) + ${TIMEOUT}))
  while true; do
    status=$(openstack loadbalancer show $lbid -f value -c provisioning_status)
    if [[ $status == "ACTIVE" ]]; then
      if [[ $i == 2 ]]; then
        printf "\n>>>>>>> Load balancer ${lbid} is ACTIVE\n"
        break
      fi
      let i++
    else
      i=0
    fi

    sleep 10
    now=$(date +%s)
    [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Timeout when waiting for the load balancer ${lbid} ACTIVE\n" && exit 1
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
        [ $now -gt $end ] && printf "\n>>>>>>> FAIL: Failed to wait for the Service ${service_name} deleted\n" && exit 1
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
    wait_for_service_address ${service}
    wait_address_accessible $ipaddr

    printf "\n>>>>>>> Sending request to the Service ${service}\n"
    podname=$(curl -sS http://${ipaddr} | grep Hostname | awk -F':' '{print $2}' | cut -d ' ' -f2)
    if [[ "$podname" =~ "echoserver" ]]; then
        printf "\n>>>>>>> Expected: Get correct response from Service ${service}\n"
    else
        printf "\n>>>>>>> FAIL: Get incorrect response from Service ${service}, expected: echoserver, actual: $podname\n"
        curl -sS http://${ipaddr}
        exit 1
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
    local public_ip=$(curl -sS ifconfig.me)
    local local_ip=$(ip route get 8.8.8.8 | head -1 | awk '{print $7}')

    if [[ ${OCTAVIA_PROVIDER} == "ovn" ]]; then
        printf "\n>>>>>>> Skipping Service ${service} test for OVN provider\n"
        return 0
    fi

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
    wait_for_service_address ${service}
    wait_address_accessible $ipaddr

    printf "\n>>>>>>> Sending request to the Service ${service}\n"
    ip_in_header=$(curl -sS http://${ipaddr} | grep  x-forwarded-for | awk -F'=' '{print $2}')
    if [[ "${ip_in_header}" != "${local_ip}" && "${ip_in_header}" != "${public_ip}" && "${ip_in_header}" != "${GATEWAY_IP}" ]]; then
        printf "\n>>>>>>> FAIL: Get incorrect response from Service ${service}, ip_in_header: ${ip_in_header}, local_ip: ${local_ip}, gateway_ip: ${GATEWAY_IP}, public_ip: ${public_ip}\n"
        curl -sS http://${ipaddr}
        exit 1
    else
        printf "\n>>>>>>> Expected: Get correct response from Service ${service}\n"
    fi

    printf "\n>>>>>>> Delete Service ${service}\n"
    kubectl -n $NAMESPACE delete service ${service}
}

########################################################################
## Name: test_update_port
## Desc: Create a k8s service and update the service port/nodeport.
## Params: None
########################################################################
function test_update_port {
    local service="test-update-port"

    printf "\n>>>>>>> Creating Service %s \n" "${service}"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service}
  namespace: $NAMESPACE
  annotations:
    service.beta.kubernetes.io/openstack-internal-load-balancer: "true"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - name: port1
      protocol: TCP
      port: 80
      targetPort: 8080
    - name: port2
      protocol: TCP
      port: 8080
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service %s created \n" "${service}"
    wait_for_service_address ${service}

    printf "\n>>>>>>> Validating openstack load balancer \n"
    lbid=$(openstack loadbalancer list -c id -c name | grep "octavia-lb-test_${service}" | awk '{print $2}')
    if [[ -z $lbid ]]; then
      printf "\n>>>>>>> FAIL: Load balancer not found for Service ${service}\n"
      exit 1
    fi
    lb_info=$(openstack loadbalancer status show $lbid)
    listener_count=$(echo $lb_info | jq '.loadbalancer.listeners | length')
    member_ports=$(echo $lb_info | jq '.loadbalancer.listeners | .[].pools | .[].members | .[].protocol_port' | uniq | tr '\n' ' ')
    service_nodeports=$(kubectl -n $NAMESPACE get svc $service -o json | jq '.spec.ports | .[].nodePort' | tr '\n' ' ')

    if [[ ${listener_count} != 2 ]]; then
        printf "\n>>>>>>> FAIL: Unexpected number of listeners(${listener_count}) created for service ${service}\n"
        exit 1
    fi
    if [[ "${member_ports}" != "${service_nodeports}" ]]; then
        printf "\n>>>>>>> FAIL: Load balancer member ports %s and service nodeports %s not match\n" "${member_ports}" "${service_nodeports}"
        kubectl -n $NAMESPACE get svc $service -o yaml
        exit 1
    fi

    printf "\n>>>>>>> Expected: NodePorts ${member_ports} before updating service.\n"

    printf "\n>>>>>>> Removing port2 and update NodePort of port1.\n"
    kubectl -n $NAMESPACE patch svc $service --type json -p '[{"op": "remove","path": "/spec/ports/1"},{"op": "remove","path": "/spec/ports/0/nodePort"}]'

    printf "\n>>>>>>> Waiting for load balancer $lbid ACTIVE.\n"
    wait_for_loadbalancer $lbid

    printf "\n>>>>>>> Validating openstack load balancer after updating the service.\n"
    lb_info=$(openstack loadbalancer status show $lbid)
    listener_count=$(echo $lb_info | jq '.loadbalancer.listeners | length')
    member_port=$(echo $lb_info | jq '.loadbalancer.listeners | .[].pools | .[].members | .[].protocol_port' | uniq)
    service_nodeport=$(kubectl -n $NAMESPACE  get svc $service -o json | jq '.spec.ports | .[].nodePort')

    if [[ ${listener_count} != 1 ]]; then
        printf "\n>>>>>>> FAIL: Unexpected number of listeners(${listener_count}) for service.\n"
        exit 1
    fi
    if [[ $(echo ${member_port} | wc -l) != 1 ]]; then
        printf "\n>>>>>>> FAIL: Unexpected number of member port(${member_port}) for service.\n"
        exit 1
    fi
    if [[ ${member_port} != ${service_nodeport} ]]; then
        printf "\n>>>>>>> FAIL: Member ports ${member_port} and service nodeport ${service_nodeport} not match.\n"
        exit 1
    fi
    if [[ ${member_port} == ${member_ports} ]]; then
        printf "\n>>>>>>> FAIL: NodePort ${member_port} not changed.\n"
        exit 1
    fi

    printf "\n>>>>>>> Expected: NodePort ${member_port} after updating service.\n"

    printf "\n>>>>>>> Deleting Service ${service}\n"
    kubectl -n $NAMESPACE delete service ${service}
}

########################################################################
## Name: test_shared_lb
## Desc: The steps in this test case:
##   1. Create service-1, lb-1 created.
##   2. Create service-2 with lb-1.
##   3. Update service-2.
##   4. Delete service-2.
##   5. Create service-3 with lb-1
##   6. Create service-4 with lb-1, but with same port as service-3, should fail.
##   7. Delete service-4.
##   8. Delete service-1.
##   9. Delete service-3.
########################################################################
function test_shared_lb {
    local service1="test-shared-1"
    printf "\n>>>>>>> Create Service ${service1}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service1}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service %s creation finished \n" ${service1}
    wait_for_service_address ${service1}

    printf "\n>>>>>>> Checking Service %s annotation\n" ${service1}
    lbID=$(_check_service_lb_annotation "${service1}")

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    service2="test-shared-2"
    printf "\n>>>>>>> Create Service ${service2}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service2}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "$lbID"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service ${service2} creation finished\n"
    wait_for_service_address ${service2}

    printf "\n>>>>>>> Checking Service %s annotation\n" ${service2}
    svc2lbID=$(_check_service_lb_annotation "${service2}")
    if [[ "$svc2lbID" != "$lbID" ]]; then
      printf "\n>>>>>>> FAIL: Service annotation loadbalancer.openstack.org/load-balancer-id not equal. $lbID != $svc2lbID\n"
      kubectl -n $NAMESPACE get svc ${service2} -o yaml
      exit 1
    fi

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service2 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service2 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    service2="test-shared-2"
    printf "\n>>>>>>> Updating Service ${service2} port\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service2}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "$lbID"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 8081
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the load balancer ${lbID} update finished\n"
    wait_for_loadbalancer $lbID

    printf "\n>>>>>>> Checking the listener number for the load balancer $lbID\n"
    listenerNum=$(openstack loadbalancer status show $lbID | jq '.loadbalancer.listeners | length')
    if [[ $listenerNum != 2 ]]; then
      printf "\n>>>>>>> FAIL: The listener number should be 2 for load balancer $lbID, actual: $listenerNum\n"
      exit 1
    fi

    printf "\n>>>>>>> Deleting Service ${service2}\n"
    kubectl -n $NAMESPACE delete svc ${service2}

    printf "\n>>>>>>> Waiting for the load balancer $lbID updated\n"
    wait_for_loadbalancer $lbID

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service2 "$tags"
    if [ $? -eq 0 ]; then
      printf "\n>>>>>>> FAIL: $service2 found in load balancer tags ($tags) \n"
      exit 1
    fi

    printf "\n>>>>>>> Checking the listener number for the load balancer $lbID\n"
    listenerNum=$(openstack loadbalancer status show $lbID | jq '.loadbalancer.listeners | length')
    if [[ $listenerNum != 1 ]]; then
      printf "\n>>>>>>> FAIL: The listener number should be 1 for load balancer $lbID, actual: $listenerNum\n"
      exit 1
    fi

    service3="test-shared-3"
    printf "\n>>>>>>> Creating Service ${service3}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service3}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "$lbID"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service ${service3} creation finished \n"
    wait_for_service_address ${service3}

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service3 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service3 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    service4="test-shared-4"
    printf "\n>>>>>>> Creating Service ${service4} with port collision \n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service4}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "$lbID"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
EOF

    sleep 10

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service3 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service3 not found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service4 "$tags"
    if [ $? -eq 0 ]; then
      printf "\n>>>>>>> FAIL: $service4 found in load balancer tags ($tags) \n"
      exit 1
    fi

    printf "\n>>>>>>> Deleting Service ${service4}\n"
    kubectl -n $NAMESPACE delete svc ${service4}
    sleep 5

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service3 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service3 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    printf "\n>>>>>>> Deleting Service ${service1}\n"
    kubectl -n $NAMESPACE delete svc ${service1}

    printf "\n>>>>>>> Waiting for the load balancer ${lbID} update finished\n"
    wait_for_loadbalancer $lbID

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -eq 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 found in load balancer tags ($tags) \n"
      exit 1
    fi
    _check_lb_tags $lbID $service3 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service3 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    printf "\n>>>>>>> Checking the listener number for the load balancer $lbID\n"
    listenerNum=$(openstack loadbalancer status show $lbID | jq '.loadbalancer.listeners | length')
    if [[ $listenerNum != 1 ]]; then
      printf "\n>>>>>>> FAIL: The listener number should be 1 for load balancer $lbID, actual: $listenerNum\n"
      exit 1
    fi

    printf "\n>>>>>>> Deleting Service ${service3}\n"
    kubectl -n $NAMESPACE delete svc ${service3}
}

########################################################################
## Name: test_shared_user_lb
## Desc: The steps in this test case:
##   1. Create a load balancer lb-1 in Octavia.
##   2. Create service-1 with lb-1.
##   3. Delete service-1.
########################################################################
function test_shared_user_lb {
    # Get subnet ID for creating the load balancer
    subid=$(openstack subnet show ${LB_SUBNET_NAME} -f value -c id)
    if [ $? -ne 0 ]; then
        printf "\n>>>>>>> FAIL: failed to get subnet ${LB_SUBNET_NAME}\n"
        exit 1
    fi

    printf "\n>>>>>>> Creating openstack load balancer: --vip-subnet-id $subid \n"
    provider_option=""
    if [[ ${OCTAVIA_PROVIDER} != "" ]]; then
        provider_option="--provider=${OCTAVIA_PROVIDER}"
    fi
    lbID=$(openstack loadbalancer create --vip-subnet-id $subid --name test_shared_user_lb -f value -c id ${provider_option})
    if [ $? -ne 0 ]; then
        printf "\n>>>>>>> FAIL: failed to create load balancer\n"
        exit 1
    fi
    printf "\n>>>>>>> Waiting for openstack load balancer $lbID ACTIVE \n"
    wait_for_loadbalancer $lbID
    printf "\n>>>>>>> Creating openstack load balancer listener \n"
    openstack loadbalancer listener create --protocol HTTP --protocol-port 80 $lbID
    printf "\n>>>>>>> Waiting for openstack load balancer $lbID ACTIVE after creating listener \n"
    wait_for_loadbalancer $lbID

    printf "\n>>>>>>> Getting an external network \n"
    extNetID=$(openstack network list --external -f value -c ID | head -1)
    if [[ -z extNetID ]]; then
        printf "\n>>>>>>> FAIL: failed to find an external network\n"
        exit 1
    fi
    fip=$(openstack floating ip create --tag occm-test -f value -c id ${extNetID})
    if [ $? -ne 0 ]; then
        printf "\n>>>>>>> FAIL: failed to create FIP\n"
        exit 1
    fi
    vip=$(openstack loadbalancer show $lbID -f value -c vip_port_id)
    openstack floating ip set --port ${vip} ${fip}

    local service1="test-shared-user-lb"
    printf "\n>>>>>>> Create Service ${service1}\n"
    cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service1}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/load-balancer-id: "$lbID"
    loadbalancer.openstack.org/enable-health-monitor: "false"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
EOF

    printf "\n>>>>>>> Waiting for the Service ${service1} creation finished\n"
    wait_for_service_address ${service1}

    printf "\n>>>>>>> Checking Service %s annotation\n" ${service1}
    lbID=$(_check_service_lb_annotation "${service1}")

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -ne 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 not found in load balancer tags ($tags) \n"
      exit 1
    fi

    printf "\n>>>>>>> Deleting Service ${service1}\n"
    kubectl -n $NAMESPACE delete svc ${service1}
    printf "\n>>>>>>> Waiting for Service ${service1} deleted \n"
    wait_for_service_deleted ${service1}

    printf "\n>>>>>>> Validating tags of openstack load balancer %s \n" "$lbID"
    tags=$(openstack loadbalancer show $lbID -f value -c tags)
    tags=$(echo $tags)
    _check_lb_tags $lbID $service1 "$tags"
    if [ $? -eq 0 ]; then
      printf "\n>>>>>>> FAIL: $service1 still found in load balancer tags ($tags) \n"
      exit 1
    fi
}


########################################################################
## Name: test_preserve_and_reused_fip
## Desc: The steps in this test case:
##   1. Create load balanccer with loadbalancer.openstack.org/keep-floatingip: "true" annotation
##   2. Delete the load balancer
##   3. Check if the floating IP is preserved after deletion
##   4. Create new load balancer reassigning the floating IP allocated in point 1.
##   5. Delete the service and the allocated floating ip.
########################################################################
function test_preserve_and_reused_fip {
	local service="test-preserve-fip"
	local second_service="test-reuse-fip"

	printf "\n>>>>>>> Test - preserving Floating IP\n"
	printf ">>>>>>> Create Service ${service}\n"
	cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/keep-floatingip: "true"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF

	printf "\n>>>>>>> Waiting for the Service ${service} creation finished\n"
	wait_for_service_address ${service}
	printf "\n>>>>>>> Getting the floating IP of the service\n"
	FIP=`kubectl -n $NAMESPACE get svc test-preserve-fip --no-headers| awk '{print $4}'`
	printf "\n>>>>>>> Checking if Floating IP is in openstack"
	FIP_IN_OS=`openstack floating ip list --floating-ip-address "$FIP" -f json | jq length`
	if [ "$FIP_IN_OS" -ne "1" ]; then
	    printf "The floating IP doesn't exist in openstack"
	    exit 1
	fi

	printf "\n>>>>>>> Delete Service ${service}\n"
	kubectl -n $NAMESPACE delete service ${service}

	printf "\n>>>>>>> Checking if Floating IP is still in openstack"
	FIP_IN_OS=`openstack floating ip list --floating-ip-address "$FIP" -f json | jq length`
	if [ "$FIP_IN_OS" -ne "1" ]; then
	    printf "The floating IP doesn't exist in openstack"
	    exit 1
	fi

		printf "\n>>>>>>> Creating new service with the same floating ip"
	
	cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${second_service}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/keep-floatingip: "true"
    loadbalancer.openstack.org/load-balancer-address: "$FIP"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF
	printf "\n>>>>>>> Waiting for the Service ${second_service} creation finished\n"
	wait_for_service_address ${second_service}
	
	printf "\n>>>>>>> Delete Service ${second_service}\n"
	kubectl -n $NAMESPACE delete service ${second_service}

	printf "\n>>>>>>> Delete floating IP ${FIP_IN_OS}\n"
	FIP_ID=`openstack floating ip list --floating-ip-address "$FIP" -f json | jq '.[0].ID' | tr -d '"'`
	openstack floating ip delete $FIP_ID
}
########################################################################
## Name: test_healthmonitors
## Desc: The steps in this test case:
##   1. Create load balanccer with healthmonitor with non-default values
##   2. Using the openstack CLI check if:
##    2.1 The load balancer has health monitor
##    2.2 The health monitor matches the specification provided in the service via annotation
##   3. Delete the service
########################################################################
function test_healthmonitors {
	local service="test-healthmonitor"


	printf "\n>>>>>>> Test - Creating healthmonitor\n"
	printf ">>>>>>> Create Service ${service}\n"
	cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: ${service}
  namespace: $NAMESPACE
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "true"
    loadbalancer.openstack.org/health-monitor-delay: "61"
    loadbalancer.openstack.org/health-monitor-timeout: "32"
    loadbalancer.openstack.org/health-monitor-max-retries: "7"
    loadbalancer.openstack.org/health-monitor-max-retries-down: "2"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
EOF

	printf "\n>>>>>>> Waiting for the Service ${service} creation finished\n"
	wait_for_service_address ${service}
	printf "\n>>>>>>> Checking if Octavia health monitor is matching spec\n"
	
	HM_ID=`openstack loadbalancer healthmonitor list | grep "$service" | awk '{print $2}'`
	HM=`openstack loadbalancer healthmonitor show "$HM_ID" -f json`
	
	HM_DELAY=`echo "$HM" | jq '.delay'`
	HM_MAX_RETRIES=`echo "$HM" | jq '.max_retries'`
	HM_TIMEOUT=`echo "$HM" | jq '.timeout'`
	HM_MAX_RETRIES_DOWN=`echo "$HM" | jq '.max_retries_down'`
	
	if [ "$HM_DELAY" -ne "61" ]; then
	    printf "The healthmonitor DELAY doesn't match the one in the service specification"
	    exit 1
	fi
	if [ "$HM_MAX_RETRIES" -ne "7" ]; then
	    printf "The healthmonitor MAX RETRIES doesn't match the one in the service specification"
	    exit 1
	fi
	if [ "$HM_TIMEOUT" -ne "32" ]; then
	    printf "The healthmonitor TIMEOUT doesn't match the one in the service specification"
	    exit 1
	fi
	if [ "$HM_MAX_RETRIES_DOWN" -ne "2" ]; then
	    printf "The healthmonitor MAX RETRIES DOWN doesn't match the one in the service specification"
	    exit 1
	fi
	
	printf "\n>>>>>>> Delete Service ${service}\n"
	kubectl -n $NAMESPACE delete service ${service}
}
create_namespace
create_deployment
set_openstack_credentials

test_basic
test_forwarded
test_update_port
test_shared_lb
test_shared_user_lb
test_preserve_and_reused_fip
test_healthmonitors
