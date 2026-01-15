/*
Copyright 2026.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ModeType defines the build mode type
// +kubebuilder:validation:Enum=docker;buildpack
type ModeType string

const (
	ModeTypeDocker    ModeType = "docker"
	ModeTypeBuildpack ModeType = "buildpack"
)

// DockerConfig defines Docker build configuration
type DockerConfig struct {
	// envVars are environment variables for the build
	// +optional
	EnvVars []corev1.EnvVar `json:"envVars,omitempty"`

	// path is the path to Dockerfile
	// +required
	Path string `json:"path"`

	// repoUrl is the Git repository URL
	// +required
	RepoURL string `json:"repoUrl"`

	// revision is the Git branch/tag/commit
	// +required
	Revision string `json:"revision"`
}

// BuildMode defines how to build the image
type BuildMode struct {
	// type specifies the build type
	// +required
	// +kubebuilder:validation:Required
	Type ModeType `json:"type"`

	// docker configuration (only if type=docker)
	// +optional
	Docker *DockerConfig `json:"docker,omitempty"`
}

// AplTeamBuildSpec defines the desired state of AplTeamBuild
type AplTeamBuildSpec struct {
	// externalRepo indicates if this uses an external repository
	// +optional
	// +kubebuilder:default:=false
	ExternalRepo bool `json:"externalRepo,omitempty"`

	// imageName is the name of the Docker image to build
	// +required
	ImageName string `json:"imageName"`

	// mode defines the build configuration
	// +required
	Mode BuildMode `json:"mode"`

	// scanSource enables source code scanning
	// +optional
	// +kubebuilder:default:=false
	ScanSource bool `json:"scanSource,omitempty"`

	// tag is the image tag
	// +required
	Tag string `json:"tag"`

	// trigger enables automatic builds
	// +optional
	// +kubebuilder:default:=false
	Trigger bool `json:"trigger,omitempty"`
}

// AplTeamBuildStatus defines the observed state of AplTeamBuild
type AplTeamBuildStatus struct {
	// buildJob is a reference to the current build Job
	// +optional
	BuildJob *corev1.ObjectReference `json:"buildJob,omitempty"`

	// lastBuildTime is when the last build was triggered
	// +optional
	LastBuildTime *metav1.Time `json:"lastBuildTime,omitempty"`

	// imageDigest is the digest of the last built image
	// +optional
	ImageDigest string `json:"imageDigest,omitempty"`

	// conditions represent the current state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=atb
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.imageName`
// +kubebuilder:printcolumn:name="Tag",type=string,JSONPath=`.spec.tag`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AplTeamBuild is the Schema for the aplteambuilds API
type AplTeamBuild struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AplTeamBuildSpec   `json:"spec,omitempty"`
	Status AplTeamBuildStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AplTeamBuildList contains a list of AplTeamBuild
type AplTeamBuildList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AplTeamBuild `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AplTeamBuild{}, &AplTeamBuildList{})
}
