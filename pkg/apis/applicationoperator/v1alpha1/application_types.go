package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationSpec defines the desired state of Application
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environment`,description=`The environment in which the application lives`
// +kubebuilder:printcolumn:name="Version",type=integer,JSONPath=`.spec.version`,description=`The version of the application`
type ApplicationSpec struct {
	// +kubebuilder:validation:MaxLength=24
	Application string `json:"application"`
	// +kubebuilder:validation:MaxLength=10
	Environment string `json:"environment"`
	Version     string `json:"version"`
	Method      string `json:"method,omitempty"`
}

// ApplicationStatus defines the observed state of Application
// +kubebuilder:printcolumn:name="Config",type=string,JSONPath=`.status.configVersion`,description=`The version of the last applied configuration`
// +kubebuilder:printcolumn:name="Last Updated",type=date-time,JSONPath=`.status.lastUpdated`,description=`The time the application was last updated`
type ApplicationStatus struct {
	ConfigVersion string      `json:"configVersion"`
	LastUpdated   metav1.Time `json:"lastUpdated"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Application is the Schema for the applications API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=applications,scope=Namespaced
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
