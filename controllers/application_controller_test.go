package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	applicationoperatorgithubiov1alpha1 "github.com/application-operator/application-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sanity-io/litter"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestVersionToRFC1123(t *testing.T) {
	got := versionToRFC1123("hello-world", 20)
	if got != "hello-world" {
		t.Errorf("expected \"hello-world\", got %s", got)
	}
	got = versionToRFC1123("hello-world", 10)
	if got != "hello-worl" {
		t.Errorf("expected \"hello-worl\", got %s", got)
	}
	got = versionToRFC1123("hello-world", 6)
	if got != "hello" {
		t.Errorf("expected \"hello\", got %s", got)
	}
	got = versionToRFC1123("hello.1234", 10)
	if got != "hello-1234" {
		t.Errorf("expected \"hello-1234\", got %s", got)
	}
	got = versionToRFC1123("hello-----", 10)
	if got != "hello" {
		t.Errorf("expected \"hello\", got %s", got)
	}
}

func TestEnvVarsToMap(t *testing.T) {
	result := envVarsToMap()
	_, ok := result["PATH"]
	if !ok {
		t.Errorf("Expected environment variables to contain PATH")
	}
}

var _ = Describe("Application Operator controller", func() {

	//
	// Creates a new application object.
	//
	createApplication := func(applicationName string, applicationNamespace string, ctx context.Context) (types.NamespacedName, *applicationoperatorgithubiov1alpha1.Application) {
		//
		// Setup for a test application object.
		//
		application := &applicationoperatorgithubiov1alpha1.Application{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "application-operator.github.io/v1alpha1",
				Kind:       "Application",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      applicationName,
				Namespace: applicationNamespace,
			},
			Spec: applicationoperatorgithubiov1alpha1.ApplicationSpec{
				Application: applicationName,
				Environment: "dev",
				Version:     "1",
				Method:      "test-method",
				DryRun:      false,
			},
		}

		//
		// Create an application object.
		//
		Expect(k8sClient.Create(ctx, application)).Should(Succeed())

		applicationKey := types.NamespacedName{Name: applicationName, Namespace: applicationNamespace}
		createdApplication := &applicationoperatorgithubiov1alpha1.Application{}

		//
		// Check that the application object is creeted.
		//
		Eventually(func() bool {
			err := k8sClient.Get(ctx, applicationKey, createdApplication)
			return err == nil
		}).Should(BeTrue())

		return applicationKey, createdApplication
	}

	//
	// Defines a predicate function used to check the status of the application.
	//
	type checkFn func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool

	//
	// Checks for an expected status of the aplication.
	// Fails the test if the expected state is not found.
	//
	expectApplicationStatus := func(applicationKey types.NamespacedName, ctx context.Context, check checkFn) *applicationoperatorgithubiov1alpha1.Application {

		createdApplication := &applicationoperatorgithubiov1alpha1.Application{}

		Eventually(func() (bool, error) {
			err := k8sClient.Get(ctx, applicationKey, createdApplication)
			if err != nil {
				return false, err
			}
			return check(createdApplication.Status), nil
		}, 5).Should(Equal(true))

		return createdApplication
	}

	//
	// Checks we have one active deployment.
	//
	checkSingleActiveDeployment := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Active) == 1
	}

	//
	// Checks there are no active deployments.
	//
	checkZeroActiveDeployments := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Active) == 0
	}

	//
	// Checks there is one successful deployment.
	//
	checkSuccessfulDeployment := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Succeeded) == 1
	}

	//
	// Checks we have zero failed deployments.
	//
	checkZeroFailedDeployments := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Failed) == 0
	}

	//
	// Checks there is one failed deployment.
	//
	checkFailedDeployment := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Failed) == 1
	}

	//
	// Checks we have zero sucessful deployments.
	//
	checkZeroSuccessfulDeployments := func(status applicationoperatorgithubiov1alpha1.ApplicationStatus) bool {
		return len(status.Succeeded) == 0
	}

	It("initiates a deployment when an application object is created", func(done Done) {
		applicationName := "my-app-1"
		applicationNamespace := "default"

		ctx := context.Background()

		applicationKey, _ := createApplication(applicationName, applicationNamespace, ctx)

		expectApplicationStatus(applicationKey, ctx, checkSingleActiveDeployment)

		close(done)
	}, 7)

	It("updates status when deployment has succeeded", func(done Done) {
		applicationName := "my-app-2"
		applicationNamespace := "default"

		ctx := context.Background()

		applicationKey, _ := createApplication(applicationName, applicationNamespace, ctx)

		createdApplication := expectApplicationStatus(applicationKey, ctx, checkSingleActiveDeployment)

		//
		// Extract the job name.
		//
		activeJobName := createdApplication.Status.Active[0].Name

		//
		// Get the details for the deployment job.
		//
		deploymentJobKey := types.NamespacedName{Name: activeJobName, Namespace: applicationNamespace}
		createdDeploymentJob := &batchv1.Job{}

		getErr := k8sClient.Get(ctx, deploymentJobKey, createdDeploymentJob)
		if getErr != nil {
			fmt.Println("Error:")
			fmt.Println(getErr)
			litter.Dump(getErr)
			Fail("Failed due to error")
		}

		//
		// Mark the job as completed.
		//
		createdDeploymentJob.Status.Conditions = append(createdDeploymentJob.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: v1.ConditionTrue,
		})

		//
		// Fake a status update of the deployment job to indicate that it succeeeded.
		// This update triggers the application operator reconciler.
		//
		updateErr := k8sClient.Status().Update(ctx, createdDeploymentJob)
		if updateErr != nil {
			fmt.Println("Error:")
			fmt.Println(updateErr)
			Fail("Failed due to error")
		}

		//
		// Wait until we have zero active deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkZeroActiveDeployments)

		//
		// There should be one successful deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkSuccessfulDeployment)

		//
		// There should be zero failed deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkZeroFailedDeployments)

		close(done)
	}, 7)

	It("updates status when deployment has failed", func(done Done) {
		applicationName := "my-app-3"
		applicationNamespace := "default"

		ctx := context.Background()

		applicationKey, _ := createApplication(applicationName, applicationNamespace, ctx)

		createdApplication := expectApplicationStatus(applicationKey, ctx, checkSingleActiveDeployment)

		//
		// Extract the job name.
		//
		activeJobName := createdApplication.Status.Active[0].Name

		//
		// Get the details for the deployment job.
		//
		deploymentJobKey := types.NamespacedName{Name: activeJobName, Namespace: applicationNamespace}
		createdDeploymentJob := &batchv1.Job{}

		getErr := k8sClient.Get(ctx, deploymentJobKey, createdDeploymentJob)
		if getErr != nil {
			fmt.Println("Error:")
			fmt.Println(getErr)
			litter.Dump(getErr)
			Fail("Failed due to error")
		}

		//
		// Mark the job as completed.
		//
		createdDeploymentJob.Status.Conditions = append(createdDeploymentJob.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobFailed,
			Status: v1.ConditionTrue,
		})

		//
		// Fake a status update of the deployment job to indicate that it succeeeded.
		// This update triggers the application operator reconciler.
		//
		updateErr := k8sClient.Status().Update(ctx, createdDeploymentJob)
		if updateErr != nil {
			fmt.Println("Error:")
			fmt.Println(updateErr)
			Fail("Failed due to error")
		}

		//
		// Wait until we have zero active deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkZeroActiveDeployments)

		//
		// There should be one failed deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkFailedDeployment)

		//
		// There should be zero successful deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkZeroSuccessfulDeployments)

		close(done)
	}, 7)

	It("invokes webhook when deployment has completed", func(done Done) {
		applicationName := "my-app-4"
		applicationNamespace := "default"

		ctx := context.Background()

		//
		// Reset webhooks count.
		//
		NumWebhooksInvoked = 0
		os.Setenv("WEBHOOK_START", "A")
		os.Setenv("WEBHOOK_COMPLETION", "B")

		applicationKey, _ := createApplication(applicationName, applicationNamespace, ctx)

		createdApplication := expectApplicationStatus(applicationKey, ctx, checkSingleActiveDeployment)

		//
		// Extract the job name.
		//
		activeJobName := createdApplication.Status.Active[0].Name

		//
		// Get the details for the deployment job.
		//
		deploymentJobKey := types.NamespacedName{Name: activeJobName, Namespace: applicationNamespace}
		createdDeploymentJob := &batchv1.Job{}

		getErr := k8sClient.Get(ctx, deploymentJobKey, createdDeploymentJob)
		if getErr != nil {
			fmt.Println("Error:")
			fmt.Println(getErr)
			litter.Dump(getErr)
			Fail("Failed due to error")
		}

		//
		// Mark the job as completed.
		//
		createdDeploymentJob.Status.Conditions = append(createdDeploymentJob.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: v1.ConditionTrue,
		})

		//
		// Fake a status update of the deployment job to indicate that it succeeeded.
		// This update triggers the application operator reconciler.
		//
		updateErr := k8sClient.Status().Update(ctx, createdDeploymentJob)
		if updateErr != nil {
			fmt.Println("Error:")
			fmt.Println(updateErr)
			Fail("Failed due to error")
		}

		//
		// There should be one successful deployments.
		//
		expectApplicationStatus(applicationKey, ctx, checkSuccessfulDeployment)

		Expect(NumWebhooksInvoked).Should(Equal(2))

		close(done)
	}, 7)
})
