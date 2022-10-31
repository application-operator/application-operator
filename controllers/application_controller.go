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

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	applicationoperatorgithubiov1alpha1 "github.com/application-operator/application-operator/api/v1alpha1"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The key to index on to find jobs owned by an application instance.
var jobOwnerKey = ".metadata.controller"

// Callback function to invoke a webhook.
type invokeWebhookFn func(url string, payload map[string]string) ([]byte, error)

// ReconcileApplication reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	InvokeWebhook invokeWebhookFn
	Queue         chan batchv1.Job
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

	log.Infof("reconciling Application %s/%s", request.Namespace, request.Name)

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
		log.Errorf("couldn't get application object: %v", err)
		return reconcile.Result{}, err
	}
	log.Infof("found Application %s/%s version %d", instance.Name, instance.Namespace, instance.Generation)

	//
	// Find Jobs for the application instance.
	//
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.InNamespace(request.Namespace), client.MatchingFields{jobOwnerKey: request.Name}); err != nil {
		log.Errorf("couldn't list jobs: %v", err)
		return ctrl.Result{}, err
	}

	name := jobName(instance)
	found := false

	for _, job := range jobs.Items {
		if job.Name != name {
			log.Infof("deleting job %s", job.Name)
			r.Delete(ctx, &job)
			continue
		}
		found = true
		_, finishedType := isJobFinished(&job)
		switch finishedType {
		case "":
			if instance.Status.Status != "running" {
				instance.Status.Status = "running"
				instance.Status.LastUpdated = metav1.Time{Time: time.Now()}
			}
		case batchv1.JobFailed:
			if instance.Status.Status != "failed" {
				instance.Status.Status = "failed"
				_, err := r.triggerCompletionWebhook(job, "Failed") // The job has transitioned to failed, trigger failed web hook.
				if err != nil {
					log.Errorf("couldn't send failure webhook: %v", err)
					return reconcile.Result{}, err
				}
				instance.Status.LastUpdated = metav1.Time{Time: time.Now()}
			}
		case batchv1.JobComplete:
			if instance.Status.Status != "succeeded" {
				instance.Status.Status = "succeeded"
				_, err := r.triggerCompletionWebhook(job, "Succeeded") // The job has transitioned to success, trigger success web hook.
				if err != nil {
					log.Errorf("couldn't send success webhook: %v", err)
					return reconcile.Result{}, err
				}
				instance.Status.LastUpdated = metav1.Time{Time: time.Now()}
			}
		}
	}

	if found {
		if err := r.Status().Update(ctx, instance); err != nil {
			log.Errorf("unable to update Application status: %v", err)
			return ctrl.Result{}, err
		}

		// Job already exists - don't requeue
		log.Infof("skip reconcile: Job %s/%s already exists", request.Namespace, name)
		return reconcile.Result{}, nil
	}

	//
	// Define a new Job object
	//
	job, err := r.newJobForApplication(instance)
	if err != nil {
		log.Errorf("couldn't create new job object: %v", err)
		return reconcile.Result{}, err
	}

	// Set Application instance as the owner and controller
	if err = controllerutil.SetControllerReference(instance, job, r.Scheme); err != nil {
		log.Errorf("couldn't set %s as owner of job: %v", instance.Name, err)
		return reconcile.Result{}, err
	}

	log.Infof("Creating a new Job %s/%s", job.Namespace, job.Name)

	err = r.Client.Create(context.TODO(), job)
	if err != nil {
		log.Errorf("couldn't create job %s: %v", job.Name, err)
		return reconcile.Result{}, err
	}
	instance.Status.LastUpdated = metav1.Time{Time: time.Now()}
	instance.Status.Status = "created"
	instance.Status.JobID = job.Labels["job-id"]
	instance.Status.JobName = name

	if err := r.Status().Update(ctx, instance); err != nil {
		log.Errorf("unable to update Application status: %v", err)
		return ctrl.Result{}, err
	}

	// Job created successfully - don't requeue
	return reconcile.Result{}, nil
}

// Determines if a Job has finished.
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

func jobName(application *applicationoperatorgithubiov1alpha1.Application) string {
	return fmt.Sprintf("%s-%s-%s-%s",
		versionToRFC1123(application.Spec.Environment, 13),
		versionToRFC1123(application.Spec.Application, 13),
		versionToRFC1123(os.Getenv("CONFIG_VERSION"), 13),
		versionToRFC1123(application.Spec.Version, 13),
	)
}

func (r *ApplicationReconciler) newJobForApplication(application *applicationoperatorgithubiov1alpha1.Application) (*batchv1.Job, error) {
	env := envVarsToMap()
	// Note: strings below are truncated to fix the Kubernetes name length of 253 characters.

	bJobId, err := r.triggerStartWebhook(application)
	if err != nil {
		return nil, err
	}
	jobId := string(bJobId)
	if jobId == "" {
		jobId = uuid.New().String()
	}

	templateVars := &TemplateVars{
		Application: application,
		Env:         env,
		JobName:     jobName(application),
	}
	method := application.Spec.Method
	if method == "" {
		method = "default"
	}
	templateDir, ok := templateVars.Env["TEMPLATE_DIR"]
	if !ok {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("cannot get working directory")
		}
		templateDir = path.Join(wd, "templates")
	}
	t, err := template.ParseFiles(path.Join(templateDir, fmt.Sprintf("%s-template.yml", method)))
	if err != nil {
		return nil, fmt.Errorf("error reading template file: %v", err)
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, templateVars)
	if err != nil {
		return nil, fmt.Errorf("error templating file: %v", err)
	}
	var job batchv1.Job
	err = yaml.Unmarshal(buf.Bytes(), &job)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert template to job: %v", err)
	}

	if job.Labels == nil {
		job.Labels = map[string]string{}
	}

	job.Labels["job-id"] = jobId

	if job.Spec.ActiveDeadlineSeconds == nil {
		configuredDeadline := env["DEPLOYMENT_DEADLINE"]

		var deadline int64
		if configuredDeadline != "" {
			parsed, err := strconv.Atoi(env["DEPLOYMENT_DEADLINE"])
			if err != nil {
				return nil, fmt.Errorf("failed to parse integer deadline (seconds) from DEPLOYMENT_DEADLINE environment variable")
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
		// Watch Application resources, but ignore Application.Status changes
		For(&applicationoperatorgithubiov1alpha1.Application{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Also watch Job resources (we want to know about Job Status to track success/failure in Application)
		Owns(&batchv1.Job{}).
		Complete(r)
}

// Triggers the post-deployment webhook to notify if the webhook succeeded or failed.
func (r *ApplicationReconciler) triggerCompletionWebhook(job batchv1.Job, eventType string) ([]byte, error) {
	env := envVarsToMap()
	webhookUrl := env["WEBHOOK_COMPLETION"]
	if webhookUrl != "" {
		webhookPayload := map[string]string{
			"eventType":     eventType,
			"id":            job.Labels["job-id"],
			"environment":   job.Labels["Environment"],
			"application":   job.Labels["Application"],
			"configVersion": job.Labels["ConfigVersion"],
			"version":       job.Labels["version"],
		}
		return r.InvokeWebhook(webhookUrl, webhookPayload)
	}
	return nil, nil
}

func (r *ApplicationReconciler) triggerStartWebhook(application *applicationoperatorgithubiov1alpha1.Application) ([]byte, error) {
	env := envVarsToMap()
	webhookUrl := env["WEBHOOK_START"]
	if webhookUrl != "" {
		webhookPayload := map[string]string{
			"environment":   application.Spec.Environment,
			"application":   application.Spec.Application,
			"configVersion": env["CONFIG_VERSION"],
			"version":       application.Spec.Version,
		}
		return r.InvokeWebhook(webhookUrl, webhookPayload)

	}
	return nil, nil
}

// Makes a HTTP post request.

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
