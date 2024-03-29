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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationSpec defines the desired state of Application
type ApplicationSpec struct {
	// +kubebuilder:validation:MaxLength=24
	Application string `json:"application"`
	// +kubebuilder:validation:MaxLength=10
	Environment string `json:"environment"`
	Version     string `json:"version,omitempty"`
	Method      string `json:"method,omitempty"`
	// +kubebuilder:default:=false
	DryRun bool `json:"dryrun,omitempty"`
}

// ApplicationStatus defines the observed state of Application
type ApplicationStatus struct {
	LastUpdated   metav1.Time `json:"lastUpdated,omitempty"`
	Status        string      `json:"status,omitempty"`
	JobID         string      `json:"jobId,omitempty"`
	JobName       string      `json:"jobName,omitempty"`
	ConfigVersion string      `json:"configVersion,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Application is the Schema for the applications API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=applications,scope=Namespaced
// +kubebuilder:printcolumn:name="Application",type=string,JSONPath=`.spec.application`,description=`The name of the application`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environment`,description=`The environment in which the application lives`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`,description=`The version of the application`
// +kubebuilder:printcolumn:name="Config",type=string,JSONPath=`.status.configVersion`,description=`The version of the last applied configuration`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`,description=`The status of the application job`
// +kubebuilder:printcolumn:name="Last Updated",type=date,JSONPath=`.status.lastUpdated`,description=`The time the application was last updated`
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ApplicationList contains a list of Application
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
