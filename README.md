
## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy Controller on the cluster

1) Apply the Installer YAML to the Cluster
~~~
kubectl apply -f dist/install.yaml
~~~
2) Apply CR file

~~~
kubectl apply -f config/samples/application_v1alpha1_flashjob.yaml
~~~

