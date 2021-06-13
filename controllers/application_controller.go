/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	applicationoperatorgithubiov1alpha1 "github.com/application-operator/application-operator/api/v1alpha1"
)

var log = logf.Log.WithName("controller_application")

// ReconcileApplication reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=application-operator.github.io.application-operator.github.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=application-operator.github.io.application-operator.github.io,resources=applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=application-operator.github.io.application-operator.github.io,resources=applications/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Application")

	// Fetch the Application instance
	instance := &applicationoperatorgithubiov1alpha1.Application{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Define a new Pod object
	job, err := newJobForApplication(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Set Application instance as the owner and controller
	if err = controllerutil.SetControllerReference(instance, job, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Job already exists
	found := &batchv1.Job{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
		err = r.Client.Create(context.TODO(), job)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Job created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Job already exists - don't requeue
	reqLogger.Info("Skip reconcile: Job already exists", "Job.Namespace", found.Namespace, "Job.Name", found.Name)
	return reconcile.Result{}, nil
}

type TemplateVars struct {
	Application *applicationoperatorgithubiov1alpha1.Application
	Env         map[string]string
	JobName     string
}

func envVarsToMap() map[string]string {
	environ := os.Environ()
	result := make(map[string]string, len(environ))
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		result[pair[0]] = pair[1]
	}
	return result
}

func versionToRFC1123(version string, length int) string {
	format := fmt.Sprintf("%%.%ds", length)
	return strings.TrimRight(fmt.Sprintf(format, strings.ReplaceAll(strings.ReplaceAll(version, ".", "-"), "_", "-")), "-")
}

func newJobForApplication(application *applicationoperatorgithubiov1alpha1.Application) (*batchv1.Job, error) {
	env := envVarsToMap()
	jobName := fmt.Sprintf("%s-%s-%s-%s", application.Spec.Environment, application.Spec.Application, versionToRFC1123(env["CONFIG_VERSION"], 13), versionToRFC1123(application.Spec.Version, 13))
	templateVars := &TemplateVars{
		Application: application,
		Env:         env,
		JobName:     jobName,
	}
	method := application.Spec.Method
	if method == "" {
		method = "default"
	}
	templateDir, ok := templateVars.Env["TEMPLATE_DIR"]
	if !ok {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("Cannot get working directory")
		}
		templateDir = path.Join(wd, "templates")
	}
	t, err := template.ParseFiles(path.Join(templateDir, fmt.Sprintf("%s-template.yml", method)))
	if err != nil {
		return nil, fmt.Errorf("Error reading template file: %v", err)
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, templateVars)
	if err != nil {
		return nil, fmt.Errorf("Error templating file: %v", err)
	}
	var job batchv1.Job
	err = yaml.Unmarshal(buf.Bytes(), &job)
	if err != nil {
		return nil, fmt.Errorf("Couldn't convert template to job: %v", err)
	}
	return &job, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&applicationoperatorgithubiov1alpha1.Application{}).
		Complete(r)
}
