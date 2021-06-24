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
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ref "k8s.io/client-go/tools/reference"
)

//
// The key to index on to find jobs owned by an application instance.
//
var jobOwnerKey = ".metadata.controller"

var log = logf.Log.WithName("controller_application")

//
// Callback function to invoke a webhook.
//
type invokeWebhookFn func(url string, payload map[string]string) ([]byte, error)

// ReconcileApplication reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	InvokeWebhook invokeWebhookFn
}

//+kubebuilder:rbac:groups=application-operator.github.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=application-operator.github.io,resources=applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=application-operator.github.io,resources=applications/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;create;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	if r.InvokeWebhook == nil {
		r.InvokeWebhook = httpPost
	}

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

	//
	// Find Jobs for the application instance.
	//
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.InNamespace(request.Namespace), client.MatchingFields{jobOwnerKey: request.Name}); err != nil {
		return ctrl.Result{}, err
	}

	var activeJobs []*batchv1.Job
	var successfulJobs []*batchv1.Job
	var failedJobs []*batchv1.Job

	//
	// Sort Jobs into active and completed.
	//
	for _, job := range jobs.Items {
		_, finishedType := isJobFinished(&job)
		switch finishedType {
		case "":
			activeJobs = append(activeJobs, &job)
		case batchv1.JobFailed:
			failedJobs = append(failedJobs, &job)
			if !containsJob(instance.Status.Failed, job.Name, job.Namespace) {
				r.triggerDeploymentWebhook(job, "Failed") // The job has transitioned to failed, trigger failed web hook.
			}

		case batchv1.JobComplete:
			successfulJobs = append(successfulJobs, &job)
			if !containsJob(instance.Status.Succeeded, job.Name, job.Namespace) {
				r.triggerDeploymentWebhook(job, "Succeeded") // The job has transitioned to success, trigger success web hook.
			}
		}
	}

	//
	// Update status of the Application instance.
	//

	instance.Status.LastUpdated = metav1.Time{Time: time.Now()}

	instance.Status.Active = nil
	for _, activeJob := range activeJobs {
		jobRef, err := ref.GetReference(r.Scheme, activeJob)
		if err != nil {
			log.Error(err, "Failed to get reference to job", "job", activeJob)
			continue
		}
		instance.Status.Active = append(instance.Status.Active, *jobRef)
	}

	instance.Status.Succeeded = nil
	for _, successfulJob := range successfulJobs {
		jobRef, err := ref.GetReference(r.Scheme, successfulJob)
		if err != nil {
			log.Error(err, "Failed to get reference to job", "job", successfulJob)
			continue
		}
		instance.Status.Succeeded = append(instance.Status.Succeeded, *jobRef)
	}

	instance.Status.Failed = nil
	for _, failedJob := range failedJobs {
		jobRef, err := ref.GetReference(r.Scheme, failedJob)
		if err != nil {
			log.Error(err, "Failed to get reference to job", "job", failedJob)
			continue
		}
		instance.Status.Failed = append(instance.Status.Failed, *jobRef)
	}

	//
	// Save the status of the application.
	//
	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "unable to update Application status")
		return ctrl.Result{}, err
	}

	//
	// Define a new Job object
	//
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

		//
		// Job doesn't exist, create it.
		//
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

//
// Determines if a Job has finished.
//
func isJobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}

	return false, ""
}

type TemplateVars struct {
	Application *applicationoperatorgithubiov1alpha1.Application
	Env         map[string]string
	JobName     string
	JobId       string
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
	// Note: strings below are truncated to fix the Kubernetes name length of 253 characters.
	jobName := fmt.Sprintf("%s-%s-%s-%s",
		versionToRFC1123(application.Spec.Environment, 13),
		versionToRFC1123(application.Spec.Application, 13),
		versionToRFC1123(env["CONFIG_VERSION"], 13),
		versionToRFC1123(application.Spec.Version, 13),
	)

	templateVars := &TemplateVars{
		Application: application,
		Env:         env,
		JobName:     jobName,
		JobId:       jobId,
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

	if job.Labels == nil {
		job.Labels = map[string]string{}
	}

	job.Labels["Environment"] = application.Spec.Environment
	job.Labels["Application"] = application.Spec.Application
	job.Labels["ConfigVersion"] = env["CONFIG_VERSION"]
	job.Labels["ApplicationVersion"] = application.Spec.Version

	if job.Spec.ActiveDeadlineSeconds == nil {
		configuredDeadline := env["DEPLOYMENT_DEADLINE"]

		var deadline int64
		if configuredDeadline != "" {
			parsed, err := strconv.Atoi(env["DEPLOYMENT_DEADLINE"])
			if err != nil {
				return nil, fmt.Errorf("Failed to parse integer deadline (seconds) from DEPLOYMENT_DEADLINE environment variable")
			}
			deadline = int64(parsed)
		} else {
			deadline = 10 * 60 // Default to 10 minutes.
		}

		job.Spec.ActiveDeadlineSeconds = &deadline
	}

	return &job, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {

	//
	// Create an index that we can use to look up jobs later.
	//
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &batchv1.Job{}, jobOwnerKey, func(rawObj client.Object) []string {
		job := rawObj.(*batchv1.Job)         // Get object.
		owner := metav1.GetControllerOf(job) // Get owner.

		if owner == nil {
			// No owner is found.
			return nil
		}

		if owner.Kind != "Application" { //todo: do i need to check the api version?
			// The owner is not an application.
			return nil
		}

		return []string{owner.Name}
	})

	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).

		// Tells the operator framework the type to watch.
		For(&applicationoperatorgithubiov1alpha1.Application{}).

		// Inform the manager this controller owns some job, automatically call Reconcile when a job changes.
		Owns(&batchv1.Job{}).
		Complete(r)
}

//
// Triggers the post-deployment webhook to notify if the webhook succeeded or failed.
//
func (r *ApplicationReconciler) triggerDeploymentWebhook(job batchv1.Job, eventType string) ([]byte, error) {
	env := envVarsToMap()
	webhookUrl := env["WEBHOOK"]
	if webhookUrl != "" {
		webhookPayload := map[string]string{
			"eventType":          eventType,
			"environment":        job.Labels["Environment"],
			"application":        job.Labels["Application"],
			"configVersion":      job.Labels["ConfigVersion"],
			"applicationVersion": job.Labels["ApplicationVersion"],
		}
		return r.InvokeWebhook(webhookUrl, webhookPayload)
	}
	return nil, nil
}

//
// Makes a HTTP post request.
//
func httpPost(url string, payload map[string]string) ([]byte, error) {
	postBody, _ := json.Marshal(payload)
	requestBody := bytes.NewBuffer(postBody)

	response, err := http.Post(url, "application/json", requestBody)
	if err != nil {
		return nil, err
	}
	var buffer []byte
	_, err = response.Body.Read(buffer)
	return buffer, err
}

//
// Check if an array contains a particular named job.
//
func containsJob(jobs []corev1.ObjectReference, jobName string, jobNamespace string) bool {

	for _, job := range jobs {
		if job.Name == jobName && job.Namespace == jobNamespace {
			return true
		}
	}

	return false
}
