# application-operator

application-operator relies on a few things being explicitly set

* `CONFIG_VERSION` environment variable - typically `git rev-parse HEAD` or `git describe --tags` make
  good examples for this. It's expected that whatever automation maintains the operator keeps this value
  updated


## Installation

* Install the Custom Resource Definition (see deploy/)
* Install the operator (see deploy/deploy.yaml)
* Set up applications by creating new Application resources (see deploy/example.yaml)
* Add RBAC permissions for the ServiceAccount for the job

You will need a container that can do deployments. The example job template assumes
that the container can run `/bin/deploy application environment version` and that will
do the trick. The underlying mechanism is up to you, but I'll provide an example
container that uses ansible-runner to do the work.

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


## Developer notes

This operator was built using kubebuilder
