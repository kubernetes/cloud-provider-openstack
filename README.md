# Cloud Provider OpenStack Helm Chart Repository

## Add repo

    helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
    helm repo update

## Install Cinder CSI chart

    helm install cinder-csi cpo/openstack-cinder-csi

## Install Manila CSI chart

    helm install manila-csi cpo/openstack-manila-csi
