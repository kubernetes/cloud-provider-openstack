module k8s.io/cloud-provider-openstack

go 1.16

require (
	github.com/MichaelTJones/walk v0.0.0-20161122175330-4748e29d5718 // indirect
	github.com/container-storage-interface/spec v1.5.0
	github.com/golang/protobuf v1.5.2
	github.com/gophercloud/gophercloud v0.21.0
	github.com/gophercloud/utils v0.0.0-20210909165623-d7085207ff6d
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/go-version v1.2.0
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.6.1
	github.com/kubernetes-csi/csi-test v2.2.0+incompatible
	github.com/kubernetes-csi/csi-test/v4 v4.2.0
	github.com/mgutz/str v1.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/mapstructure v1.1.2
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.4
	github.com/pborman/uuid v1.2.0
	github.com/pelletier/go-toml v1.4.0 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/net v0.0.0-20210520170846-37e1c6afe023
	golang.org/x/sys v0.0.0-20210616094352-59db8d763f22
	google.golang.org/grpc v1.38.0
	gopkg.in/gcfg.v1 v1.2.3
	gopkg.in/godo.v2 v2.0.9
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.0
	k8s.io/apimachinery v0.22.0
	k8s.io/apiserver v0.22.0
	k8s.io/client-go v0.22.0
	k8s.io/cloud-provider v0.22.0
	k8s.io/component-base v0.22.0
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.9.0
	k8s.io/kubernetes v1.22.0
	k8s.io/mount-utils v0.22.0
	k8s.io/utils v0.0.0-20210707171843-4b05e18ac7d9
	software.sslmate.com/src/go-pkcs12 v0.0.0-20190209200317-47dd539968c4
)

replace (
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.0.0-rc9
	google.golang.org/grpc v1.34.0 => google.golang.org/grpc v1.29.0
	k8s.io/api => k8s.io/api v0.22.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.22.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.0
	k8s.io/apiserver => k8s.io/apiserver v0.22.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.22.0
	k8s.io/client-go => k8s.io/client-go v0.22.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.22.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.22.0
	k8s.io/code-generator => k8s.io/code-generator v0.22.0
	k8s.io/component-base => k8s.io/component-base v0.22.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.22.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.22.0
	k8s.io/cri-api => k8s.io/cri-api v0.22.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.22.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.22.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.22.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.22.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.22.0
	k8s.io/kubectl => k8s.io/kubectl v0.22.0
	k8s.io/kubelet => k8s.io/kubelet v0.22.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.22.0
	k8s.io/metrics => k8s.io/metrics v0.22.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.22.0
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.22.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.22.0
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.22.0
	k8s.io/sample-controller => k8s.io/sample-controller v0.22.0
)
