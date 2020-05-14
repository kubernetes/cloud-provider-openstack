# Manila CSI provisioner

First add the repo:

    helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
    helm repo update

If you are using Helm v3:

    helm install manila-csi cpo/openstack-manila-csi

If you are using Helm v2:

    helm install --name manila-csi cpo/openstack-manila-csi
