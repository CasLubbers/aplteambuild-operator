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

package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	buildv1alpha1 "akamai.io/aplteambuild-operator/api/v1alpha1"
)

// AplTeamBuildReconciler reconciles a AplTeamBuild object
type AplTeamBuildReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AplTeamBuild object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *AplTeamBuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var aplTeamBuild buildv1alpha1.AplTeamBuild
	if err := r.Get(ctx, req.NamespacedName, &aplTeamBuild); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("AplTeamBuild resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get AplTeamBuild")
		return ctrl.Result{}, err
	}

	// Initialize conditions if not present
	if len(aplTeamBuild.Status.Conditions) == 0 {
		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err := r.Status().Update(ctx, &aplTeamBuild); err != nil {
			if !apierrors.IsConflict(err) {
				log.Error(err, "Failed to update AplTeamBuild status")
			}
			return ctrl.Result{}, err
		}

		// Re-fetch after status update
		if err := r.Get(ctx, req.NamespacedName, &aplTeamBuild); err != nil {
			log.Error(err, "Failed to re-fetch AplTeamBuild")
			return ctrl.Result{}, err
		}
	}

	// Check if Job for current generation exists
	jobName := fmt.Sprintf("%s-build-%d", aplTeamBuild.Name, aplTeamBuild.Generation)
	var existingJob batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: jobName}, &existingJob)

	if err != nil && apierrors.IsNotFound(err) {
		// Create new Job for this generation
		job, err := r.constructJobForBuild(&aplTeamBuild)
		if err != nil {
			log.Error(err, "Failed to construct Job")
			meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "JobCreationFailed",
				Message: fmt.Sprintf("Failed to construct job: %v", err),
			})
			if statusErr := r.Status().Update(ctx, &aplTeamBuild); statusErr != nil {
				if !apierrors.IsConflict(statusErr) {
					log.Error(statusErr, "Failed to update status")
				}
			}
			return ctrl.Result{}, err
		}

		log.Info("Creating a new Job", "Job.Name", job.Name, "Generation", aplTeamBuild.Generation)
		if err := r.Create(ctx, job); err != nil {
			log.Error(err, "Failed to create new Job")
			meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "JobCreationFailed",
				Message: fmt.Sprintf("Failed to create job: %v", err),
			})
			if statusErr := r.Status().Update(ctx, &aplTeamBuild); statusErr != nil {
				if !apierrors.IsConflict(statusErr) {
					log.Error(statusErr, "Failed to update status")
				}
			}
			return ctrl.Result{}, err
		}

		// Update status with job reference
		aplTeamBuild.Status.BuildJob = &corev1.ObjectReference{
			Name:       job.Name,
			Namespace:  job.Namespace,
			Kind:       "Job",
			APIVersion: "batch/v1",
		}
		aplTeamBuild.Status.LastBuildTime = &metav1.Time{Time: metav1.Now().Time}

		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionUnknown,
			Reason:  "Building",
			Message: fmt.Sprintf("Build job %s created", job.Name),
		})

		if err := r.Status().Update(ctx, &aplTeamBuild); err != nil {
			if apierrors.IsConflict(err) {
				// Watch will trigger reconcile with latest version
				log.V(1).Info("Status update conflict, will be retriggered by watch")
				return ctrl.Result{}, nil
			}
			// For other errors, log and return error
			log.Error(err, "Failed to update AplTeamBuild status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Job")
		return ctrl.Result{}, err
	}

	// Job exists - check its status
	isJobComplete := false
	isJobFailed := false

	for _, condition := range existingJob.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			isJobComplete = true
		} else if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			isJobFailed = true
		}
	}

	log.V(1).Info("job status", "complete", isJobComplete, "failed", isJobFailed)

	// Update status conditions based on current state
	if isJobFailed {
		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BuildFailed",
			Message: "Image build failed",
		})
	} else if isJobComplete {
		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "BuildComplete",
			Message: "Image build completed successfully",
		})
	} else {
		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionUnknown,
			Reason:  "Building",
			Message: fmt.Sprintf("Build job %s in progress", existingJob.Name),
		})
	}

	if err := r.Status().Update(ctx, &aplTeamBuild); err != nil {
		if !apierrors.IsConflict(err) {
			log.Error(err, "unable to update AplTeamBuild status")
			return ctrl.Result{}, err
		}
		log.V(1).Info("Status update conflict, will retry")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// constructJobForBuild creates a Job manifest for building the Docker image
func (r *AplTeamBuildReconciler) constructJobForBuild(aplTeamBuild *buildv1alpha1.AplTeamBuild) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-build-%d", aplTeamBuild.Name, aplTeamBuild.Generation)

	// Kaniko executor arguments
	kanikoArgs := []string{
		fmt.Sprintf("--dockerfile=%s", aplTeamBuild.Spec.Mode.Docker.Path),
		fmt.Sprintf("--context=git://%s#refs/heads/%s",
			aplTeamBuild.Spec.Mode.Docker.RepoURL,
			aplTeamBuild.Spec.Mode.Docker.Revision,
		),
		fmt.Sprintf("--destination=%s:%s", aplTeamBuild.Spec.ImageName, aplTeamBuild.Spec.Tag),
		"--no-push",
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: aplTeamBuild.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "aplteambuild",
				"app.kubernetes.io/instance":   aplTeamBuild.Name,
				"app.kubernetes.io/managed-by": "aplteambuild-operator",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "kaniko",
							Image: "gcr.io/kaniko-project/executor:latest",
							Args:  kanikoArgs,
							Env:   aplTeamBuild.Spec.Mode.Docker.EnvVars,
						},
					},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(aplTeamBuild, job, r.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AplTeamBuildReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&buildv1alpha1.AplTeamBuild{}).
		Owns(&batchv1.Job{}).
		Named("aplteambuild").
		Complete(r)
}
