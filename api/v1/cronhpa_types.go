/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type Job struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	// job will only run once if enabled.
	RunOnce    bool  `json:"runOnce,omitempty"`
	TargetSize int32 `json:"targetSize"`
}

type ScaleTargetRef struct {
	ApiVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// CronHPASpec defines the desired state of CronHPA
type CronHPASpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//// Foo is an example field of CronHPA. Edit cronhpa_types.go to remove/update
	//Foo string `json:"foo,omitempty"`
	ExcludeDates   []string       `json:"excludeDates,omitempty"`
	ScaleTargetRef ScaleTargetRef `json:"scaleTargetRef"`
	Jobs           []Job          `json:"jobs"`
}

type JobState string

const (
	Succeed   JobState = "Succeed"
	Failed    JobState = "Failed"
	Submitted JobState = "Submitted"
)

// Condition defines the condition of CronHPA
type Condition struct {
	// Type of job condition, Complete or Failed.
	Name string `json:"name"`

	JobId string `json:"jobId"`

	Schedule string `json:"schedule"`

	TargetSize int32 `json:"targetSize"`

	RunOnce bool `json:"runOnce"`

	State JobState `json:"state"`

	LastProbeTime metav1.Time `json:"lastProbeTime"`

	// Human readable message indicating details about last transition.
	// +optional
	Message string `json:"message"`
}

// CronHPAStatus defines the observed state of CronHPA
type CronHPAStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ScaleTargetRef ScaleTargetRef `json:"scaleTargetRef,omitempty"`
	ExcludeDates   []string       `json:"excludeDates,omitempty"`
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CronHPA is the Schema for the cronhpas API
type CronHPA struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CronHPASpec   `json:"spec,omitempty"`
	Status CronHPAStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CronHPAList contains a list of CronHPA
type CronHPAList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronHPA `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronHPA{}, &CronHPAList{})
}
