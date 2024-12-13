package controller

import (
	"context"
	flashv1alpha1 "flashjob/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type FlashJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const flashJobFinalizer = "flashjob.finalizers.flashjob.nbfc.io"

func (r *FlashJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// List Akri instances
	akriList := &unstructured.UnstructuredList{}
	akriList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "akri.sh",
		Version: "v0",
		Kind:    "Instance",
	})

	if err := r.List(ctx, akriList, &client.ListOptions{}); err != nil {
		logger.Error(err, "Failed to list Akri instances")
		return ctrl.Result{}, err
	}

	// Process each Akri instance
	for _, item := range akriList.Items {
		name, _, _ := unstructured.NestedString(item.Object, "metadata", "name")
		uid, _, _ := unstructured.NestedString(item.Object, "metadata", "uid")

		brokerProps, exists, _ := unstructured.NestedMap(item.Object, "spec", "brokerProperties")
		if exists {
			hostEndpoint, _ := brokerProps["HOST_ENDPOINT"].(string)
			firmware, _ := brokerProps["FIRMWARE"].(string)
			device, _ := brokerProps["DEVICE"].(string)
			authUser, _ := brokerProps["AUTH_USER"].(string)
			authPass, _ := brokerProps["AUTH_PASS"].(string)
			newType, _ := brokerProps["NEW_TYPE"].(string)

			logger.Info("Akri instance details",
				"name", name,
				"uid", uid,
				"hostEndpoint", hostEndpoint,
				"firmware", firmware,
				"device", device,
				"authUser", authUser,
				"authPass", authPass,
				"newType", newType)
		}
	}

	var flashJob flashv1alpha1.FlashJob
	if err := r.Get(ctx, req.NamespacedName, &flashJob); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("FlashJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get FlashJob")
		return ctrl.Result{}, err
	}

	if !flashJob.ObjectMeta.DeletionTimestamp.IsZero() {
		if containsString(flashJob.GetFinalizers(), flashJobFinalizer) {
			foundPod := &corev1.Pod{}
			err := r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-flashing-pod", Namespace: flashJob.Namespace}, foundPod)
			if err == nil {
				logger.Info("Deleting associated Pod", "Pod.Namespace", foundPod.Namespace, "Pod.Name", foundPod.Name)
				if err := r.Delete(ctx, foundPod); err != nil {
					logger.Error(err, "Failed to delete Pod")
					return ctrl.Result{}, err
				}
			}

			flashJob.SetFinalizers(removeString(flashJob.GetFinalizers(), flashJobFinalizer))
			if err := r.Update(ctx, &flashJob); err != nil {
				logger.Error(err, "Failed to remove finalizer from FlashJob")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !containsString(flashJob.GetFinalizers(), flashJobFinalizer) {
		logger.Info("Adding Finalizer for the FlashJob")
		flashJob.SetFinalizers(append(flashJob.GetFinalizers(), flashJobFinalizer))
		if err := r.Update(ctx, &flashJob); err != nil {
			logger.Error(err, "Failed to update FlashJob with finalizer")
			return ctrl.Result{}, err
		}
	}

	foundPod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-flashing-pod", Namespace: flashJob.Namespace}, foundPod)
	if err != nil && errors.IsNotFound(err) {
		pod := r.createFlashPod(&flashJob)
		logger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.Create(ctx, pod)
		if err != nil {
			logger.Error(err, "Failed to create new Pod")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get Pod")
		return ctrl.Result{}, err
	}

	if foundPod.Status.Phase == corev1.PodSucceeded {
		flashJob.Status.Phase = "Completed"
		flashJob.Status.Message = "Firmware flashing completed successfully"
	} else if foundPod.Status.Phase == corev1.PodFailed {
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Firmware flashing failed"
	} else {
		flashJob.Status.Phase = "InProgress"
		flashJob.Status.Message = "Firmware flashing in progress"
	}

	if err := r.Status().Update(ctx, &flashJob); err != nil {
		logger.Error(err, "Failed to update FlashJob status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *FlashJobReconciler) createFlashPod(flashJob *flashv1alpha1.FlashJob) *corev1.Pod {
	labels := map[string]string{
		"app": flashJob.Name,
	}

	var hostEndpoint string
	hostEndpoint = "http://operator-default-endpoint:8080"

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      flashJob.Name + "-flashing-pod",
			Namespace: flashJob.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "flash-container",
				Image: flashJob.Spec.Firmware + ":" + flashJob.Spec.Version,
				Env: []corev1.EnvVar{
					{Name: "FIRMWARE", Value: flashJob.Spec.Firmware},
					{Name: "UUID", Value: flashJob.Spec.UUID},
					{Name: "HOST_ENDPOINT", Value: hostEndpoint},
					{Name: "APPLICATION_TYPE", Value: flashJob.Spec.ApplicationType},
					{Name: "VERSION", Value: flashJob.Spec.Version},
					{Name: "DEVICE", Value: flashJob.Spec.Device},
				},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

func (r *FlashJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flashv1alpha1.FlashJob{}).
		Owns(&corev1.Pod{}).
		WithOptions(controller.Options{}).
		Complete(r)
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
