module github.com/application-operator/application-operator

go 1.13

require (
	github.com/dabealu/zookeeper-exporter v0.1.8 // indirect
	github.com/operator-framework/operator-sdk v0.18.0
	github.com/prometheus/prometheus v2.5.0+incompatible // indirect
	github.com/spf13/pflag v1.0.5
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.18.2 // Required by prometheus-operator
)
