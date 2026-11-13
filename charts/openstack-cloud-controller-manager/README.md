# openstack-cloud-controller-manager

Deploys the OpenStack Cloud Controller Manager to your cluster.

Default configuration values are the same as the CCM itself.

## How To install

You need to configure an `openstack-ccm.yaml` values file with at least:

- `cloudConfig.global.auth-url` with the Keystone URL
- Authentication
  - with password: `cloudConfig.global.username` and `cloudconfig.global.password`
  - with application credentials: (`cloudConfig.global.application-credential-id` or `cloudConfig.global.application-credential-name`) and `cloudConfig.global.application-credential-secret`
- Load balancing
  - `cloudConfig.loadBalancer.floating-network-id` **or**
  - `cloudConfig.loadBalancer.floating-subnet-id` **or**
  - `cloudConfig.loadBalancer.floating-subnet`

If you want to enable health checks for your Load Balancers (optional), set `cloudConfig.loadBalancer.create-monitor: true`.

Then run:

```sh
helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
helm repo update
helm install openstack-ccm cpo/openstack-cloud-controller-manager --values openstack-ccm.yaml
```

## DNS during cluster bootstrap

The chart defaults to `hostNetwork: true` and `dnsPolicy: ClusterFirst`.
For host-networked Pods, `ClusterFirst` uses the node's DNS configuration, so OCCM can resolve OpenStack API endpoints before cluster DNS is available.
CoreDNS may be waiting for OCCM to initialise nodes and remove the `node.cloudprovider.kubernetes.io/uninitialized` taint.
Making OCCM depend on CoreDNS in this situation can prevent the cluster from starting.
The node DNS servers must be able to resolve the OpenStack API endpoints.

If cluster DNS is available independently of OCCM, you can set `dnsPolicy: ClusterFirstWithHostNet` with `hostNetwork: true`.
When disabling host networking, the default `ClusterFirst` policy uses cluster DNS instead of node DNS.
This requires working pod networking and cluster DNS, for example when running only the `service` controller on an existing cluster.

When adopting this chart's defaults during an upgrade, deployments with `hostNetwork: true` that previously used `ClusterFirstWithHostNet` will switch to node DNS.
To keep using cluster DNS with host networking, explicitly set `dnsPolicy: ClusterFirstWithHostNet` in your Helm values.
Deployments with `hostNetwork: false` continue to use cluster DNS with the default `ClusterFirst` policy.
Explicit `dnsPolicy` overrides continue to apply.

## Using an external secret

In order to use an external secret for the OCCM:

```yaml
secret:
  enabled: true
  name: cloud-config
  create: false
```

Create the secret with:

```sh
kubectl create secret -n kube-system generic cloud-config --from-file=./cloud.conf
```

## Tolerations

To deploy OCCM to worker nodes only (e.g. when the controlplane is isolated), adjust the tolerations in the chart:

```yaml
tolerations:
  - key: node.cloudprovider.kubernetes.io/uninitialized
    value: "true"
    effect: NoSchedule
```

## Unsupported configurations

- The chart does not support the mounting of custom `clouds.yaml` files. Therefore, the following config values in the `[Global]` section won’t have any effect:
  - `use-clouds`
  - `clouds-file`
  - `cloud`
