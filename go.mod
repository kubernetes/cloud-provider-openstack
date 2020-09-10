module k8s.io/cloud-provider-openstack

go 1.15

require (
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/container-storage-interface/spec v1.2.0
	github.com/coreos/go-systemd v0.0.0-20190620071333-e64a0ec8b42a // indirect
	github.com/emicklei/go-restful v2.9.6+incompatible // indirect
	github.com/golang/protobuf v1.4.2
	github.com/gophercloud/gophercloud v0.11.1-0.20200518183226-7aec46f32c19
	github.com/gophercloud/utils v0.0.0-20200423144003-7c72efc7435d
	github.com/gorilla/mux v1.7.3
	github.com/hashicorp/go-version v1.2.0
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.6.1
	github.com/kubernetes-csi/csi-test v2.2.0+incompatible
	github.com/kubernetes-csi/csi-test/v3 v3.1.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/mapstructure v1.1.2
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/pborman/uuid v1.2.0
	github.com/pelletier/go-toml v1.4.0 // indirect
	github.com/prometheus/client_golang v1.7.1
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.4.0
	github.com/stretchr/testify v1.5.1
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sys v0.0.0-20200622214017-ed371f2e16b4
	google.golang.org/grpc v1.27.0
	gopkg.in/gcfg.v1 v1.2.3
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.19.0
	k8s.io/apimachinery v0.19.0
	k8s.io/apiserver v0.19.0
	k8s.io/client-go v0.19.0
	k8s.io/cloud-provider v0.19.0
	k8s.io/component-base v0.19.0
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.2.0
	k8s.io/kubernetes v1.19.0
	k8s.io/utils v0.0.0-20200729134348-d5654de09c73
	software.sslmate.com/src/go-pkcs12 v0.0.0-20190209200317-47dd539968c4
)

replace (
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.0.0-rc9
	k8s.io/api => k8s.io/api v0.19.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.19.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.0
	k8s.io/apiserver => k8s.io/apiserver v0.19.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.19.0
	k8s.io/client-go => k8s.io/client-go v0.19.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.19.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.19.0
	k8s.io/code-generator => k8s.io/code-generator v0.19.0
	k8s.io/component-base => k8s.io/component-base v0.19.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.19.0
	k8s.io/cri-api => k8s.io/cri-api v0.19.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.19.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.19.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.19.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.19.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.19.0
	k8s.io/kubectl => k8s.io/kubectl v0.19.0
	k8s.io/kubelet => k8s.io/kubelet v0.19.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.19.0
	k8s.io/metrics => k8s.io/metrics v0.19.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.19.0
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.19.0
	k8s.io/sample-controller => k8s.io/sample-controller v0.19.0
)
