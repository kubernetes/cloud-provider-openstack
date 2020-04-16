#!/usr/bin/env bash

OS_CONTEXT_NAME=${OS_CONTEXT_NAME:-""}
AUTH_POLICY_CONFIGMAP=${AUTH_POLICY_CONFIGMAP:-""}
ROLE_MAPPING_CONFIGMAP=${ROLE_MAPPING_CONFIGMAP:-""}

function log {
  local msg=$1
  printf "\n>>>>>>> ${FUNCNAME[1]}: ${msg}\n"
}

########################################################################
# Desc: Test authorization policy that grant users with member role in
#       project demo can get/list pods in default namespace.
# Params: N/A
########################################################################
function test_auth_policy {
  log "Update configmap ${AUTH_POLICY_CONFIGMAP}"

  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${AUTH_POLICY_CONFIGMAP}
  namespace: kube-system
data:
  policies: |
    [
      {
        "users": {
          "projects": ["demo"],
          "roles": ["member"]
        },
        "resource_permissions": {
          "default/pods": ["get", "list"]
        }
      }
    ]
EOF

  # The default value of '--authorization-webhook-cache-authorized-ttl' is 5min
  local end=$((SECONDS+300))
  while true; do
    kubectl --context ${OS_CONTEXT_NAME} get pod
    [ $? -eq 0 ] && break || true
    [ $SECONDS -gt $end ] && log "FAIL: OpenStack user should be able to get pods" && exit -1
    sleep 5
  done

  kubectl --context ${OS_CONTEXT_NAME} -n kube-system get pod
  if [ $? -eq 0 ]; then
    log "FAIL: OpenStack user should not be able to get pods in kube-system namespace"
    exit -1
  fi

  log "PASS"
}

########################################################################
# Desc: Test the role mappings that give the Keystone users with role of
#       member the group name member
# Params: N/A
########################################################################
function test_auth_role_mapping {
  log "Update configmap ${ROLE_MAPPING_CONFIGMAP}"

  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${ROLE_MAPPING_CONFIGMAP}
  namespace: kube-system
data:
  syncConfig: |
    role-mappings:
      - keystone-role: member
        groups: ["member"]
EOF

  kubectl create role mytest --verb=get,list --resource=deployments
  kubectl create rolebinding mytest --role=mytest --group=member

  # The default value of '--authorization-webhook-cache-authorized-ttl' is 5min
  local end=$((SECONDS+300))
  while true; do
    kubectl --context ${OS_CONTEXT_NAME} get deployments
    [ $? -eq 0 ] && break || true
    [ $SECONDS -gt $end ] && log "FAIL: OpenStack user should be able to get deployments" && exit -1
    sleep 5
  done

  kubectl --context ${OS_CONTEXT_NAME} -n kube-system get deployments
  if [ $? -eq 0 ]; then
    log "FAIL: OpenStack user should not be able to get deployments in kube-system namespace"
    exit -1
  fi

  kubectl delete role mytest
  kubectl delete rolebinding mytest
  log "PASS"
}

test_auth_policy
test_auth_role_mapping