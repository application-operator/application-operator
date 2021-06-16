# application-operator

application-operator relies on a few things being explicitly set

* `CONFIG_VERSION` environment variable - typically `git rev-parse HEAD` or `git describe --tags` make
  good examples for this. It's expected that whatever automation maintains the operator keeps this value
  updated


## Installation

* Create the `applications` namespace:
  ```
  kubectl create namespace applications
  ```
* Make sure you are using `applications` as your default namespace.
  ```
  kubens applications 
  ```
* Install the Custom Resource Definition:
  ```
  kubectl create namespace applications
  ```
* Alternately apply the CRD like this:
  ```
  make install
  ```
* Add the sample configuration (this also includes some RBAC to do the job. Here we only allow 
  it to modify resources in the `applications` namespace but typically you'll want a ClusterRole):
  ```
  kubectl apply -k config/samples
  ```
* Install the operator (for some reason `kubectl apply -k` fails but `kustomize | kubectl` works:
  ```
  kustomize build config/default | kubectl apply -f -
  ```
* Alternately run the operator locally:
  ```
  make run
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

* NOTE: To run locally using `make run` I had to copy `demo-template.yaml` to a `templates` sub-directory.
* NOTE: The env vars on `.env` need to be mapped to local environment variables.

* Run tests

  ```bash
  export CONFIG_VERSION=1243
  make test
  ```

## Using a bespoke application template

You will need a container that can do deployments. The example job template assumes
that the container can run `./deploy.sh resource application environment version` and that will
do the trick. The underlying mechanism is up to you - 
[Skedulo/application-operator-demo](https://github.com/Skedulo/application-operator-demo)
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

## Setting a webhook

A webhook can be used for notification of deployments that have succeeded or failed.

To set the webhook set the WEBHOOK environment variable for the application operator:

```bash
export WEBHOOK=http://host/path
```

When a deployment has completed a HTTP POST request is sent to the URL configured for the webhook with a payload that looks like this:

```json
{
  // The type of notification:
  "eventType": "Failed" or "Succeeded",

  // Details about the job that completed:
  "environment":        "<the environment of the job>",
  "application":        "<the application that was deployed>",
  "configVersion":      "<the version of the configuration for the application>",
  "applicationVersion": "<the version of the application>"

}
```

## Setting a deadline

A default deadline can be imposed on all deployments by setting the environment variable `DEPLOYMENT_DEADLINE` to the maximum number of seconds for which a deployment should run.

For example, this allows the deployment to run for 5 minutes before it is terminated:

```bash
export DEPLOYMENT_DEADLINE=300
```

A deployment that exceeds its deadline is placed in the failed state, triggering the webhook with the even type `Failed`.
