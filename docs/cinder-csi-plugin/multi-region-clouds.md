# Multi Az/Region/Openstack Configuration

### Multi cluster Configuration file

Create a configuration file with a subsection per openstack cluster to manage (pay attention to enable ignore-volume-az in BlockStorage section).

Example of configuration with 3 regions (The default is backward compatible with mono cluster configuration but not mandatory).
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloud-config
  namespace: kube-system
type: Opaque
stringData:
  cloud.conf: |-
    [BlockStorage]
    bs-version=v3
    ignore-volume-az=True
    
    [Global]
    auth-url="https://auth.cloud.openstackcluster.region-default.local/v3"
    username="region-default-username"
    password="region-default-password"
    region="default"
    tenant-id="region-default-tenant-id"
    tenant-name="region-default-tenant-name"
    domain-name="Default"
    
    [Global "region-one"]
    auth-url="https://auth.cloud.openstackcluster.region-one.local/v3"
    username="region-one-username"
    password="region-one-password"
    region="one"
    tenant-id="region-one-tenant-id"
    tenant-name="region-one-tenant-name"
    domain-name="Default"
    
    [Global "region-two"]
    auth-url="https://auth.cloud.openstackcluster.region-two.local/v3"
    username="region-two-username"
    password="region-two-password"
    region="two"
    tenant-id="region-two-tenant-id"
    tenant-name="region-two-tenant-name"
    domain-name="Default"
```



### Create region/cloud secrets

Create a secret per openstack cluster which contains a key `cloud` and as value the subsection's name of corresponding openstack cluster in configuration file.

These secrets are referenced in storageClass definitions to identify openstack cluster associated to the storageClass.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openstack-config-region-one
  namespace: kube-system
type: Opaque
stringData:
  cloud: region-one
---
apiVersion: v1
kind: Secret
metadata:
  name: openstack-config-region-two
  namespace: kube-system
type: Opaque
stringData:
  cloud: region-two
```

### Create storage Class for dedicated cluster

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
  name: sc-region-one
allowVolumeExpansion: true
allowedTopologies:
- matchLabelExpressions:
  - key: topology.cinder.csi.openstack.org/zone
    values:
    - nova
  - key: topology.kubernetes.io/region
    values:
    - region-one
parameters:
  csi.storage.k8s.io/controller-publish-secret-name: openstack-config-region-one
  csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-publish-secret-name: openstack-config-region-one
  csi.storage.k8s.io/node-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-stage-secret-name: openstack-config-region-one
  csi.storage.k8s.io/node-stage-secret-namespace: kube-system
  csi.storage.k8s.io/provisioner-secret-name: openstack-config-region-one
  csi.storage.k8s.io/provisioner-secret-namespace: kube-system
provisioner: cinder.csi.openstack.org
reclaimPolicy: Delete
volumeBindingMode: Immediate
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc-region-two
allowVolumeExpansion: true
allowedTopologies:
- matchLabelExpressions:
  - key: topology.cinder.csi.openstack.org/zone
    values:
    - nova
  - key: topology.kubernetes.io/region
    values:
    - region-two
parameters:
  csi.storage.k8s.io/controller-publish-secret-name: openstack-config-region-two
  csi.storage.k8s.io/controller-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-publish-secret-name: openstack-config-region-two
  csi.storage.k8s.io/node-publish-secret-namespace: kube-system
  csi.storage.k8s.io/node-stage-secret-name: openstack-config-region-two
  csi.storage.k8s.io/node-stage-secret-namespace: kube-system
  csi.storage.k8s.io/provisioner-secret-name: openstack-config-region-two
  csi.storage.k8s.io/provisioner-secret-namespace: kube-system
provisioner: cinder.csi.openstack.org
reclaimPolicy: Delete
volumeBindingMode: Immediate
```

### Create a csi-cinder-nodeplugin daemonset per cluster openstack

Daemonsets should deploy pods on nodes from proper openstack context. We suppose that the node have a label `topology.kubernetes.io/region` with the openstack cluster name as value (you could manage this with kubespray, manually, whatever, it should be great to implement this in openstack cloud controller manager).

Do as follows:
- Use nodeSelector to match proper nodes labels
- Add cli argument `--additionnal-topology topology.kubernetes.io/region=region-one`, which should match node labels, to container cinder-csi-plugin
- Add cli argument `--cloud-name="region-one"`, which should match configuration file subsection name, to container cinder-csi-plugin.

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: csi-cinder-nodeplugin-region-one
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: csi-cinder-nodeplugin-region-one
  template:
    metadata:
      labels:
        app: csi-cinder-nodeplugin-region-one
    spec:
      containers:
      - name: node-driver-registrar
        ...
      - name: liveness-probe
        ...
      - name: cinder-csi-plugin
        image: docker.io/k8scloudprovider/cinder-csi-plugin:v1.31.0
        args:
        - /bin/cinder-csi-plugin
        - --endpoint=$(CSI_ENDPOINT)
        - --cloud-config=$(CLOUD_CONFIG)
        - --cloud-name="region-one"
        - --additionnal-topology
        - topology.kubernetes.io/region=region-one
        env:
        - name: CSI_ENDPOINT
          value: unix://csi/csi.sock
        - name: CLOUD_CONFIG
          value: /etc/config/cloud.conf
        ...
        volumeMounts:
        ...
        - mountPath: /etc/config
          name: secret-cinderplugin
          readOnly: true
        ...
      nodeSelector:
        topology.kubernetes.io/region: region-one
      volumes:
      ...
      - name: secret-cinderplugin
        secret:
          defaultMode: 420
          secretName: cloud-config
      ...
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: csi-cinder-nodeplugin-region-two
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: csi-cinder-nodeplugin-region-two
  template:
    metadata:
      labels:
        app: csi-cinder-nodeplugin-region-two
    spec:
      containers:
      - name: node-driver-registrar
        ...
      - name: liveness-probe
        ...
      - name: cinder-csi-plugin
        image: docker.io/k8scloudprovider/cinder-csi-plugin:v1.31.0
        args:
        - /bin/cinder-csi-plugin
        - --endpoint=$(CSI_ENDPOINT)
        - --cloud-config=$(CLOUD_CONFIG)
        - --cloud-name="region-two"
        - --additionnal-topology
        - topology.kubernetes.io/region=region-two
        env:
        - name: CSI_ENDPOINT
          value: unix://csi/csi.sock
        - name: CLOUD_CONFIG
          value: /etc/config/cloud.conf
        ...
        volumeMounts:
        ...
        - mountPath: /etc/config
          name: secret-cinderplugin
          readOnly: true
        ...
      nodeSelector:
        topology.kubernetes.io/region: region-two
      volumes:
      ...
      - name: secret-cinderplugin
        secret:
          defaultMode: 420
          secretName: cloud-config
      ...
```

### Configure csi-cinder-controllerplugin deployment

Enable Topology feature-gate on container csi-provisioner of csi-cinder-controllerplugin deployment by adding cli argument ``--feature-gates="Topology=true"

Add cli argument `--cloud-name="region-one"` for each managed openstack cluster, name should match configuration file subsection name, to container `cinder-csi-plugin`.


```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
  name: csi-cinder-controllerplugin
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: csi-cinder-controllerplugin
  template:
    metadata:
      labels:
        app: csi-cinder-controllerplugin
    spec:
      containers:
      - name: csi-provisioner
        image: registry.k8s.io/sig-storage/csi-provisioner:v3.0.0
        args:
        - --csi-address=$(ADDRESS)
        - --timeout=3m
        - --default-fstype=ext4
        - --extra-create-metadata
        - --feature-gates
        - Topology=true
        ...
      - name: cinder-csi-plugin
        image: docker.io/k8scloudprovider/cinder-csi-plugin:v1.31.0
        args:
        - /bin/cinder-csi-plugin
        - --endpoint=$(CSI_ENDPOINT)
        - --cloud-config=$(CLOUD_CONFIG)
        - --cluster=$(CLUSTER_NAME)
        - --cloud-name="region-one"
        - --cloud-name="region-two"
        env:
        - name: CSI_ENDPOINT
          value: unix://csi/csi.sock
        - name: CLOUD_CONFIG
          value: /etc/config/cloud.conf
        - name: CLUSTER_NAME
          value: kubernetes
        volumeMounts:
        - mountPath: /etc/config
          name: secret-cinderplugin
          readOnly: true
        ...
      - name: csi-attacher
        ...
      - name: csi-snapshotter
        ...
      - name: csi-resizer
        ...
      - name: liveness-probe
        ...
      volumes:
      - name: secret-cinderplugin
        secret:
          defaultMode: 420
          secretName: cloud-config
      ...
```

