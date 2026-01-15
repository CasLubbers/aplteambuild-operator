# Building the AplTeamBuild Operator

This guide walks through building a Kubernetes operator with Kubebuilder. We'll go step by step, adding code blocks and explaining why we make each decision.

## Prerequisites

```bash
go version  # Go 1.21+
docker version
kubectl version
kind create cluster  # or minikube

# Install Kubebuilder
https://book.kubebuilder.io/quick-start#installation
```

---

## Step 1: Initialize the Project

```bash
mkdir ~/workspace/linode/aplteambuild-operator
cd ~/workspace/linode/aplteambuild-operator

kubebuilder init --domain akamai.io --repo akamai.io/aplteambuild-operator
```

The `--domain` flag sets your API group domain. Your CRDs will live under `<group>.akamai.io` (like `build.akamai.io`). This prevents conflicts with other operators.

The `--repo` flag sets your Go module name. Use your actual domain or GitHub path.

Now create the API:

```bash
kubebuilder create api --group build --version v1alpha1 --kind AplTeamBuild --resource --controller
# Answer yes to both prompts
```

This creates:
- `api/v1alpha1/aplteambuild_types.go` - where you define your CRD
- `internal/controller/aplteambuild_controller.go` - where your logic goes

---

## Step 2: Define Your API

Open `api/v1alpha1/aplteambuild_types.go`. We need to define what an AplTeamBuild resource looks like.

### Define the Mode Type Enum

Add this at the top of the file after the imports:

```go
// ModeType defines the build mode type
// +kubebuilder:validation:Enum=docker;buildpack
type ModeType string

const (
	ModeTypeDocker    ModeType = "docker"
	ModeTypeBuildpack ModeType = "buildpack"
)
```

**Why:** The `+kubebuilder:validation:Enum` marker tells Kubernetes to only accept these values. This validation happens at the API level before your controller even sees it.

### Define Docker Configuration

```go
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
```

**Why `corev1.EnvVar`:** We reuse the standard Kubernetes type instead of creating our own. This means users already know the format from writing Pods.

**Why `+optional` vs `+required`:** These markers generate OpenAPI validation. Optional fields can be omitted in YAML, required fields must be present.

### Define Build Mode

```go
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
```

**Why a pointer for Docker:** `*DockerConfig` allows it to be nil when not using docker mode. A regular struct would always be present (with empty fields).

### Define the Spec

Replace the generated `AplTeamBuildSpec` with:

```go
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
```

**Why separate the spec fields:** Spec is what the user *wants* to happen. It's their input. We never modify spec from the controller.

### Define the Status

Replace the generated `AplTeamBuildStatus` with:

```go
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
```

**Why `ObjectReference`:** Standard way in Kubernetes to reference other objects. It includes namespace, name, kind, and apiVersion - everything you need to find the Job.

**Why `metav1.Condition`:** Standard Kubernetes pattern for status. Every resource should have conditions. Tools like kubectl understand this format.

**Why status is separate:** Status is what's actually happening - the *observed* state. Only the controller writes to status, users only read it.

### Update the Resource Markers

Find the `AplTeamBuild` struct and update its markers:

```go
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
```

**What these markers do:**
- `subresource:status` - makes status a separate subresource (prevents users from writing to it)
- `shortName=atb` - lets you use `kubectl get atb` instead of typing the full name
- `printcolumn` - customizes what `kubectl get` shows (instead of just NAME and AGE)

### Generate the Code

```bash
make manifests generate
```

This generates:
- CRD YAML in `config/crd/bases/`
- Deep copy methods in `zz_generated.deepcopy.go`

---

## Step 3: Implement the Controller

Open `internal/controller/aplteambuild_controller.go`.

### Add RBAC Markers

Add these above the `Reconcile` function:

```go
// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=build.akamai.io,resources=aplteambuilds/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

func (r *AplTeamBuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
```

**What these do:** Generate RBAC permissions. Without these, your operator can't access the resources it needs. The `make manifests` command will create the Role/RoleBinding YAML.

### Add Required Imports

At the top of the file, update the imports:

```go
import (
	"context"
	"fmt"
	"strings"
	"time"

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
```

### Step 3.1: Fetch the Resource

Inside the `Reconcile` function, replace the generated code with:

```go
func (r *AplTeamBuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the AplTeamBuild resource
	var aplTeamBuild buildv1alpha1.AplTeamBuild
	if err := r.Get(ctx, req.NamespacedName, &aplTeamBuild); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("AplTeamBuild resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get AplTeamBuild")
		return ctrl.Result{}, err
	}
```

**Why check `IsNotFound` separately:** When a resource is deleted, reconcile gets triggered but the object is gone. This is normal - just return without error. If we returned the error, it would requeue needlessly.

**Why return `nil` for not found:** Returning an error triggers exponential backoff retry. For deletions, we don't need to retry - the object is gone.

### Step 3.2: Initialize Status Conditions

Add this after fetching the resource:

```go
	// Initialize status conditions if not present
	if len(aplTeamBuild.Status.Conditions) == 0 {
		meta.SetStatusCondition(&aplTeamBuild.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err := r.Status().Update(ctx, &aplTeamBuild); err != nil {
			if apierrors.IsConflict(err) {
				log.V(1).Info("Status update conflict during initialization, will retry")
				return ctrl.Result{}, nil
			}
			log.Error(err, "Failed to update AplTeamBuild status")
			return ctrl.Result{}, err
		}

		// Re-fetch after status update
		if err := r.Get(ctx, req.NamespacedName, &aplTeamBuild); err != nil {
			log.Error(err, "Failed to re-fetch AplTeamBuild")
			return ctrl.Result{}, err
		}
	}
```

**Why initialize conditions:** Every Kubernetes resource should have at least one condition. This is the API convention.

**Why handle `IsConflict` specially:** Conflicts happen when multiple reconciles try to update the same object. This is normal in distributed systems. Returning `nil` (not the error) lets the watch trigger a new reconcile immediately instead of waiting for exponential backoff.

**Why re-fetch:** Status updates increment `resourceVersion`. Re-fetching ensures we have the latest version for subsequent operations.

### Step 3.3: Check if Job Exists

Add this next:

```go
	// Check if Job for current generation exists
	jobName := fmt.Sprintf("%s-build-%d", aplTeamBuild.Name, aplTeamBuild.Generation)
	var existingJob batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: jobName}, &existingJob)

	if err != nil && apierrors.IsNotFound(err) {
		// Job doesn't exist, we'll create it
```

**Why use generation in the name:** Kubernetes increments `metadata.generation` every time the spec changes. Using it in the Job name means:
- Each spec edit creates a new build
- Old Jobs stick around for debugging
- No name conflicts

### Step 3.4: Create the Job

Inside the `if err != nil && apierrors.IsNotFound(err)` block:

```go
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
				log.V(1).Info("Status update conflict after job creation, will retry")
				return ctrl.Result{}, nil
			}
			log.Error(err, "Failed to update AplTeamBuild status")
			return ctrl.Result{}, err
		}

		//return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		return ctrl.Result{}, nil
```

**Why update status before and after errors:** Users should know what went wrong. Setting a condition with the error message makes it visible in `kubectl describe`.

**Why `Ready=Unknown` for building:** The three states are True (success), False (failure), and Unknown (in progress).

**Why not RequeueAfter here:** The watch on Jobs will trigger a new reconcile when the Job status changes. No need to requeue manually.

### Step 3.5: Handle When Job Already Exists

After the `if err != nil && apierrors.IsNotFound(err)` block, we add an else if:

```go
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
```

**Why check both complete and failed:** Jobs can be in one of three states: running, complete, or failed. We need to know which to set the right condition.

**Why `log.V(1)`:** This is debug-level logging. Only shows when you run with `--zap-log-level=debug`. Keeps logs clean in production.

### Step 3.6: Update Status Based on Job State

Continue adding:

```go
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
		if apierrors.IsConflict(err) {
			log.V(1).Info("Status update conflict, will be retriggered by watch")
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to update AplTeamBuild status")
		return ctrl.Result{}, err
	}

	// Requeue if job is still running
	if !isJobComplete && !isJobFailed {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}
```

**Why check status then update once:** This follows the pattern from the CronJob controller. Check all the state first, decide on conditions, then update status once at the end. More efficient than multiple updates.

**Why requeue if still running:** We want to keep checking until the Job finishes. The watch might fire when the Job completes, but if it doesn't, we'll check back in 10 seconds.

### Step 3.7: Implement constructJobForBuild

Add this helper function after `Reconcile`:

```go
func (r *AplTeamBuildReconciler) constructJobForBuild(aplTeamBuild *buildv1alpha1.AplTeamBuild) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-build-%d", aplTeamBuild.Name, aplTeamBuild.Generation)

	// Build Kaniko arguments
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
```

**Why `--no-push`:** For now, we just build. Later you can remove this and add registry credentials to actually push.

Continue:

```go
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
```

**Why `RestartPolicy: Never`:** Jobs should fail if the container fails. We don't want them to restart infinitely.

**Why pass through EnvVars:** Users might need to set build args or other environment variables. We just pass them through to Kaniko.

Now the critical part:

```go
	// Set owner reference - this is critical!
	if err := ctrl.SetControllerReference(aplTeamBuild, job, r.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}
```

**Why `SetControllerReference` is critical:** This does two things:
1. Sets up garbage collection - when you delete the AplTeamBuild, the Job gets deleted automatically
2. Makes the `.Owns()` watch work - when the Job changes, reconcile gets triggered on the parent AplTeamBuild

Without this, status would never update when the Job completes!

### Step 3.8: Setup the Manager

At the bottom of the file, replace the `SetupWithManager` function:

```go
func (r *AplTeamBuildReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&buildv1alpha1.AplTeamBuild{}).
		Owns(&batchv1.Job{}).
		Named("aplteambuild").
		Complete(r)
}
```

**What each line does:**
- `.For(&buildv1alpha1.AplTeamBuild{})` - Watch AplTeamBuild resources. Reconcile when they change.
- `.Owns(&batchv1.Job{})` - Watch Jobs that have an AplTeamBuild as owner. When they change, reconcile the parent.
- `.Named("aplteambuild")` - Controller name for metrics and logs
- `.Complete(r)` - Wire up this reconciler's Reconcile function

The `.Owns()` is what makes status updates work automatically when Jobs complete!

---

## Step 4: Test It

Generate manifests and install CRDs:

```bash
make manifests generate
make install
```

Run the operator locally:

```bash
make run
```

In another terminal, create a test build:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: build.akamai.io/v1alpha1
kind: AplTeamBuild
metadata:
  name: test-build
spec:
  imageName: test-app
  mode:
    type: docker
    docker:
      path: ./Dockerfile
      repoUrl: https://github.com/linode/apl-nodejs-helloworld.git
      revision: main
  tag: latest
EOF
```

Watch what happens:

```bash
# Terminal 1
kubectl get aplteambuilds -w

# Terminal 2
kubectl get jobs -w

# Terminal 3
kubectl logs -f job/test-build-build-1
```

Check the status:

```bash
kubectl get aplteambuild test-build -o yaml

# Look for:
# status:
#   conditions:
#   - type: Ready
#     status: "True"
#     reason: BuildComplete
```

Test spec changes:

```bash
kubectl edit aplteambuild test-build
# Change tag to "v2" and save

kubectl get jobs
# Should see a new job: test-build-build-2
```

---

## My Learnings

### The Reconcile Loop

Controllers don't react to specific events like "Job completed". Instead, reconcile runs whenever *something* changed, and you make the actual state match the desired state. This is called level-triggered (vs edge-triggered).

### Why Generation Matters

`metadata.generation` increments on every spec change. Using it in Job names means each edit triggers a new build without deleting old Jobs.

### Why Conflict Handling Matters

In distributed systems, conflicts are expected. Returning `nil` instead of `err` for conflicts lets the watch retrigger immediately instead of waiting for exponential backoff.

### Owner References Are Critical

`ctrl.SetControllerReference()` does two things:
1. Garbage collection - Jobs get deleted when parent is deleted
2. Makes `.Owns()` work - changes to Jobs trigger reconcile on parent

Without it, your operator won't work properly!

---

## Next Steps

### Push Images to Registry

Remove `--no-push` from Kaniko args and add registry credentials:

```bash
kubectl create secret docker-registry regcred \
  --docker-server=docker.io \
  --docker-username=<user> \
  --docker-password=<pass>
```

Add volume mount to the Job for `/kaniko/.docker/config.json`.

### Add Webhooks

Validate specs before they're created:

```bash
kubebuilder create webhook --group build --version v1alpha1 --kind AplTeamBuild --programmatic-validation
```

### Support More Build Types

Add cases for buildpacks or other builders by switching on `spec.mode.type`.

---

## Resources

- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Kubebuilder Best Practices](https://book.kubebuilder.io/reference/good-practices)
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)