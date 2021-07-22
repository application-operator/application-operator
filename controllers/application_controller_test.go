package controllers

import (
	"context"
	"testing"

	applicationoperatorgithubiov1alpha1 "github.com/application-operator/application-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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

	It("initiates a deployment job when an application object is created", func() {
		applicationName := "my-application"
		applicationNamespace := "default"

		ctx := context.Background()

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
				Application: "my-application",
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

		//
		// Check that we have no active jobs.
		//
		Eventually(func() (int, error) {
			err := k8sClient.Get(ctx, applicationKey, createdApplication)
			if err != nil {
				return -1, err
			}
			return len(createdApplication.Status.Active), nil
		}, 5000, 5000).Should(Equal(1))
	})
})
