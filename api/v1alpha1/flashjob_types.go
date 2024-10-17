package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FlashJobSpec defines the desired state of FlashJob
type FlashJobSpec struct {
	UUID     string `json:"uuid"`
	Firmware string `json:"firmware"`
	Version  string `json:"version"`
}

// FlashJobStatus defines the observed state of FlashJob
type FlashJobStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	Message    string             `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FlashJob is the Schema for the flashjobs API
type FlashJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FlashJobSpec   `json:"spec,omitempty"`
	Status            FlashJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FlashJobList contains a list of FlashJob
type FlashJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FlashJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FlashJob{}, &FlashJobList{})
}
