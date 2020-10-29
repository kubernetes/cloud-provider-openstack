<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Using magnum-auto-healer](#using-magnum-auto-healer)
  - [What is magnum-auto-healer](#what-is-magnum-auto-healer)
  - [magnum-auto-healer Design](#magnum-auto-healer-design)
  - [Deploying and testing magnum-auto-healer](#deploying-and-testing-magnum-auto-healer)
    - [Prerequisites](#prerequisites)
    - [Deploy magnum-auto-healer](#deploy-magnum-auto-healer)
    - [Testing magnum-auto-healer](#testing-magnum-auto-healer)
    - [magnum-auto-healer video demo](#magnum-auto-healer-video-demo)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Using magnum-auto-healer

## What is magnum-auto-healer

Kubernetes is self-healing container orchestration platform that will detect failures from your pods and redeploy that workload, however, magnum-auto-healer is a self-healing cluster management service that will automatically recover a failed master or worker node within your Magnum cluster. Basically, magnum-auto-healer ensures the running Kubernetes nodes are healthy by monitoring the nodes' status periodically, searching for unhealthy instances and triggering replacements when needed, maximizing your cluster's high availability and reliability, and protecting your application from downtime when the node it's running on fails.

The other side of concerns for Kubernetes cluster is scalability. Kubernetes [cluster-autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) can scale the worker pools in your cluster automatically to increase or decrease the number of worker nodes based on the sizing needs of the scheduled workloads. cluster-autoscaler periodically scans the cluster to adjust the number of worker nodes in response to your workload resource requests and any custom settings that you configure, such as scanning intervals. The main purpose of cluster-autoscaler is autoscaling, not autohealing. cluster-autoscaler can be deployed together with magnum-auto-healer.

Like cluster-autoscaler, magnum-auto-healer is implemented to use together with cloud providers as well, [OpenStack Magnum](https://docs.openstack.org/magnum/latest/user/) is supported by default.

## magnum-auto-healer Design

There are some considerations when we were designing the magnum-auto-healer service:

- We want to have a single component for the cluster autohealing purpose. There are already some other components out there in the community to deal with some specific tasks separately, combining them together with some customization may work, but will lead to much complexity and maintenance overhead.
- Support both master nodes and worker nodes.
- Cluster administrator is able to disable the autohealing feature on the fly, which is very important for the cluster operations like upgrade or scheduled maintenance.
- The Kubernetes cluster is not necessary to be exposed to either the public or the OpenStack control plane. For example, In Magnum, the end user may create a private cluster which is not accessible even from Magnum control services.
- The health check should be pluggable. Deployers should be able to write their own health check plugin with customized health check parameters.
- Support different cloud providers.

## Deploying and testing magnum-auto-healer

### Prerequisites

1. A multi-node cluster(3 masters and 3 workers) is created in Magnum.

    ```
    $ openstack coe cluster list
    +--------------------------------------+-----------------------------+-----------------+------------+--------------+-----------------+
    | uuid                                 | name                        | keypair         | node_count | master_count | status          |
    +--------------------------------------+-----------------------------+-----------------+------------+--------------+-----------------+
    | c418c335-0e52-42fc-bd68-baa8d264e072 | lingxian_por_test_1.12.7_ha | lingxian_laptop |          3 |            3 | CREATE_COMPLETE |
    +--------------------------------------+-----------------------------+-----------------+------------+--------------+-----------------+
    $ openstack server list --name lingxian-por-test-1-12-7-ha
    +--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
    | ID                                   | Name                                              | Status | Networks                                | Image                   | Flavor  |
    +--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
    | 908957c2-ac88-4b54-a1fc-91f9cc8f98f1 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-2 | ACTIVE | lingxian_net=10.0.10.33, 150.242.42.234 | fedora-atomic-27-x86_64 | c1.c4r8 |
    | 8f0c3ad9-caf5-45b6-bf3a-97b3bb6de623 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-0 | ACTIVE | lingxian_net=10.0.10.32, 150.242.42.233 | fedora-atomic-27-x86_64 | c1.c4r8 |
    | a6ae4cee-7cf2-4b25-89bc-a5c6cb2c364d | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 | ACTIVE | lingxian_net=10.0.10.34, 150.242.42.245 | fedora-atomic-27-x86_64 | c1.c4r8 |
    | 2af96203-cc6f-4b55-8fb2-062340207ebb | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-2 | ACTIVE | lingxian_net=10.0.10.31, 150.242.42.226 | fedora-atomic-27-x86_64 | c1.c2r4 |
    | 10bef366-b5a8-4400-b2c3-82188ec06b13 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-1 | ACTIVE | lingxian_net=10.0.10.30, 150.242.42.22  | fedora-atomic-27-x86_64 | c1.c2r4 |
    | 9c17f034-6825-4e49-b3cb-0ecddd1a8dd8 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-0 | ACTIVE | lingxian_net=10.0.10.29, 150.242.42.213 | fedora-atomic-27-x86_64 | c1.c2r4 |
    +--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
    ```

2. The kubeconfig file of the cluster is in place.

### Deploy magnum-auto-healer

It's recommended to run magnum-auto-healer service as a DaemonSet on the master nodes, the service is running in active-passive mode using leader election mechanism. There is a sample manifest file in `manifests/magnum-auto-healer/magnum-auto-healer.yaml`, you need to change some variables as needed before actually running `kubectl apply` command. The following commands are just examples:

```shell
magnum_cluster_uuid=c418c335-0e52-42fc-bd68-baa8d264e072
keystone_auth_url=https://api.nz-por-1.catalystcloud.io:5000/v3
user_id=ceb61464a3d341ebabdf97d1d4b97099
user_project_id=b23a5e41d1af4c20974bf58b4dff8e5a
password=password
region=RegionOne
image=k8scloudprovider/magnum-auto-healer:latest

cat <<EOF | kubectl apply -f -
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: magnum-auto-healer
  namespace: kube-system

---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: magnum-auto-healer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: magnum-auto-healer
    namespace: kube-system

---
kind: ConfigMap
apiVersion: v1
metadata:
  name: magnum-auto-healer-config
  namespace: kube-system
data:
  config.yaml: |
    cluster-name: ${magnum_cluster_uuid}
    dry-run: false
    monitor-interval: 15s
    check-delay-after-add: 20m
    leader-elect: true
    healthcheck:
      master:
        - type: Endpoint
          params:
            unhealthy-duration: 30s
            protocol: HTTPS
            port: 6443
            endpoints: ["/healthz"]
            ok-codes: [200]
        - type: NodeCondition
          params:
            unhealthy-duration: 1m
            types: ["Ready"]
            ok-values: ["True"]
      worker:
        - type: NodeCondition
          params:
            unhealthy-duration: 1m
            types: ["Ready"]
            ok-values: ["True"]
    openstack:
      auth-url: ${keystone_auth_url}
      user-id: ${user_id}
      project-id: ${user_project_id}
      password: ${password}
      region: ${region}

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: magnum-auto-healer
  namespace: kube-system
  labels:
    k8s-app: magnum-auto-healer
spec:
  selector:
    matchLabels:
      k8s-app: magnum-auto-healer
  template:
    metadata:
      labels:
        k8s-app: magnum-auto-healer
    spec:
      serviceAccountName: magnum-auto-healer
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
        - effect: NoExecute
          operator: Exists
      nodeSelector:
        node-role.kubernetes.io/master: ""
      containers:
        - name: magnum-auto-healer
          image: ${image}
          imagePullPolicy: Always
          args:
            - /bin/magnum-auto-healer
            - --config=/etc/magnum-auto-healer/config.yaml
            - --v
            - "2"
          volumeMounts:
            - name: config
              mountPath: /etc/magnum-auto-healer
      volumes:
        - name: config
          configMap:
            name: magnum-auto-healer-config
EOF
```

### Testing magnum-auto-healer

We could ssh into a worker node(`lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1` in this example) and stop the kubelet service to simulate the worker node failure. The node status check is covered in NodeCondition type of health check plugin(see configuration above).

```shell
$ ssh fedora@150.242.42.245
[fedora@lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 ~]$ sudo systemctl stop kubelet
```

Now waiting for the magnum-auto-healer to detect the node failure and trigger the repair process. First, you would see the unhealthy node is shutdown:

```shell
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
| ID                                   | Name                                              | Status  | Networks                                | Image                   | Flavor  |
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
| 908957c2-ac88-4b54-a1fc-91f9cc8f98f1 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-2 | ACTIVE  | lingxian_net=10.0.10.33, 150.242.42.234 | fedora-atomic-27-x86_64 | c1.c4r8 |
| a6ae4cee-7cf2-4b25-89bc-a5c6cb2c364d | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 | SHUTOFF | lingxian_net=10.0.10.34, 150.242.42.245 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 8f0c3ad9-caf5-45b6-bf3a-97b3bb6de623 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-0 | ACTIVE  | lingxian_net=10.0.10.32, 150.242.42.233 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 2af96203-cc6f-4b55-8fb2-062340207ebb | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-2 | ACTIVE  | lingxian_net=10.0.10.31, 150.242.42.226 | fedora-atomic-27-x86_64 | c1.c2r4 |
| 10bef366-b5a8-4400-b2c3-82188ec06b13 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-1 | ACTIVE  | lingxian_net=10.0.10.30, 150.242.42.22  | fedora-atomic-27-x86_64 | c1.c2r4 |
| 9c17f034-6825-4e49-b3cb-0ecddd1a8dd8 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-0 | ACTIVE  | lingxian_net=10.0.10.29, 150.242.42.213 | fedora-atomic-27-x86_64 | c1.c2r4 |
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
```

Then a new one comes up:

```shell
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
| ID                                   | Name                                              | Status  | Networks                                | Image                   | Flavor  |
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
| 31d5e246-6f40-4e14-88a9-8cd86a19c75a | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 | BUILD   |                                         | fedora-atomic-27-x86_64 | c1.c4r8 |
| 908957c2-ac88-4b54-a1fc-91f9cc8f98f1 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-2 | ACTIVE  | lingxian_net=10.0.10.33, 150.242.42.234 | fedora-atomic-27-x86_64 | c1.c4r8 |
| a6ae4cee-7cf2-4b25-89bc-a5c6cb2c364d | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 | SHUTOFF |                                         | fedora-atomic-27-x86_64 | c1.c4r8 |
| 8f0c3ad9-caf5-45b6-bf3a-97b3bb6de623 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-0 | ACTIVE  | lingxian_net=10.0.10.32, 150.242.42.233 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 2af96203-cc6f-4b55-8fb2-062340207ebb | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-2 | ACTIVE  | lingxian_net=10.0.10.31, 150.242.42.226 | fedora-atomic-27-x86_64 | c1.c2r4 |
| 10bef366-b5a8-4400-b2c3-82188ec06b13 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-1 | ACTIVE  | lingxian_net=10.0.10.30, 150.242.42.22  | fedora-atomic-27-x86_64 | c1.c2r4 |
| 9c17f034-6825-4e49-b3cb-0ecddd1a8dd8 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-0 | ACTIVE  | lingxian_net=10.0.10.29, 150.242.42.213 | fedora-atomic-27-x86_64 | c1.c2r4 |
+--------------------------------------+---------------------------------------------------+---------+-----------------------------------------+-------------------------+---------+
```

Finally, all the nodes are healthy again. In Magnum, the new node has the same IP address and hostname with the old one:

```shell
+--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
| ID                                   | Name                                              | Status | Networks                                | Image                   | Flavor  |
+--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
| 31d5e246-6f40-4e14-88a9-8cd86a19c75a | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-1 | ACTIVE | lingxian_net=10.0.10.34, 150.242.42.245 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 908957c2-ac88-4b54-a1fc-91f9cc8f98f1 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-2 | ACTIVE | lingxian_net=10.0.10.33, 150.242.42.234 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 8f0c3ad9-caf5-45b6-bf3a-97b3bb6de623 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-minion-0 | ACTIVE | lingxian_net=10.0.10.32, 150.242.42.233 | fedora-atomic-27-x86_64 | c1.c4r8 |
| 2af96203-cc6f-4b55-8fb2-062340207ebb | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-2 | ACTIVE | lingxian_net=10.0.10.31, 150.242.42.226 | fedora-atomic-27-x86_64 | c1.c2r4 |
| 10bef366-b5a8-4400-b2c3-82188ec06b13 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-1 | ACTIVE | lingxian_net=10.0.10.30, 150.242.42.22  | fedora-atomic-27-x86_64 | c1.c2r4 |
| 9c17f034-6825-4e49-b3cb-0ecddd1a8dd8 | lingxian-por-test-1-12-7-ha-bbgjts5g4xhb-master-0 | ACTIVE | lingxian_net=10.0.10.29, 150.242.42.213 | fedora-atomic-27-x86_64 | c1.c2r4 |
+--------------------------------------+---------------------------------------------------+--------+-----------------------------------------+-------------------------+---------+
```

### magnum-auto-healer video demo
You can find a video demo [here](https://youtu.be/QCf-26P0lPg).
