# application-operator

application-operator relies on a few things being explicitly set

* `CONFIG_VERSION` environment variable - typically `git rev-parse HEAD` or `git describe --tags` make
  good examples for this. It's expected that whatever automation maintains the operator keeps this value
  updated


## Installation

* Install the Custom Resource Definition:
  ```
  kubectl apply -k config/crd
  ```
* Install the operator (for some reason `kubectl apply -k` fails but `kustomize | kubectl` works:
  ```
  kustomize build config/default | kubectl apply -f -
  ```
* Add the sample configuration (this also includes some RBAC to do the job. Here we only allow 
  it to modify resources in the `applications` namespace but typically you'll want a ClusterRole):
  ```
  kubectl apply -k config/sample
  ```
* Add a test application:
  ```
  kubectl apply -f config/samples/resources/test-docker-debug.yaml
  ```
* If the test-docker-debug Job completes successfully, you should have a new test-docker-debug
  Deployment, and
  ```
  kubectl port-forward svc/test-docker-debug 8000:80
  ```
  should allow http://localhost:8000/ to show a blue docker-debug
* Doing the same but with prod-docker-debug should create two replicas, and visiting the service
  should show the green docker-debug


## Using a bespoke application template

You will need a container that can do deployments. The example job template assumes
that the container can run `./deploy.sh resource application environment version` and that will
do the trick. The underlying mechanism is up to you - 
[application-operator/application-operator-demo](https://github.com/application-operator/application-operator-demo)
container uses kustomize to generate slightly different configuration based on environment.

This container should either mount the configuration (e.g. from a git-sync sidecar
container) or be built with the configuration included. My preference is to build
the container with the configuration inside, tagged with the version of the code.

## Job Names

Job names are derived from the application name (as per `spec.application` in the resource
definition), the configuration version (from `CONFIG_VERSION`) and the application version
(`spec.version`). As Jobs have a maximum length of 63, we shorten the versions to 13
and impose a maximum length of 24 on the application name (to leave two characters for the
separators).

As Jobs only run once per job name, this means that the first 13 characters of versions
must be distinct across different versions - easy with a commit hash or `git describe --long`
but if you're `doing this-is-a-very-long-tag-prefix-$(git describe --long)` it won't work as well.

## Job Templates

Variables are available in the template through three sources:

* `.JobName` - the name of the job - `$application[:24]-$env[:10]-$config_version[:13]-$version[:13]`
* `.Application` - the resource definition of the application. Components of the application can
  be accessed using e.g. `.Application.Spec.Version`
* `.env` - a map of all the environment variables. For example, `PATH` is accessed using `{{ .env index 'PATH' }}`
