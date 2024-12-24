package controller

import (
	"context"
	flashv1alpha1 "flashjob/api/v1alpha1"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	//      "sigs.k8s.io/controller-runtime/pkg/controller"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type FlashJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const flashJobFinalizer = "flashjob.finalizers.flashjob.nbfc.io"

func (r *FlashJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Get the FlashJob resource
	var flashJob flashv1alpha1.FlashJob
	if err := r.Get(ctx, req.NamespacedName, &flashJob); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("FlashJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get FlashJob")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !flashJob.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &flashJob)
	}

	// Add finalizer if it doesn't exist
	if !containsString(flashJob.GetFinalizers(), flashJobFinalizer) {
		flashJob.SetFinalizers(append(flashJob.GetFinalizers(), flashJobFinalizer))
		if err := r.Patch(ctx, &flashJob, client.Merge); err != nil {
			logger.Error(err, "Failed to update FlashJob with finalizer")
			return ctrl.Result{}, err
		}
	}

	// Get Akri instance details for the specified UUID
	akriInstance, hostEndpoint, err := r.getAkriInstanceDetails(ctx, flashJob.Spec.UUID)
	if err != nil {
		logger.Error(err, "Failed to get Akri instance details")
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Failed to get device details"
		r.Status().Update(ctx, &flashJob)
		return ctrl.Result{}, err
	}

	// Create or update the flashing pod
	return r.handleFlashingPod(ctx, &flashJob, hostEndpoint, akriInstance)
}

func (r *FlashJobReconciler) getAkriInstanceDetails(ctx context.Context, uuid string) (*unstructured.Unstructured, string, error) {
	akriList := &unstructured.UnstructuredList{}
	akriList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "akri.sh",
		Version: "v0",
		Kind:    "Instance",
	})

	if err := r.List(ctx, akriList, &client.ListOptions{}); err != nil {
		return nil, "", err
	}

	for _, item := range akriList.Items {
		itemUID, _, _ := unstructured.NestedString(item.Object, "metadata", "uid")
		if itemUID == uuid {
			brokerProps, exists, _ := unstructured.NestedMap(item.Object, "spec", "brokerProperties")
			if exists {
				hostEndpoint, _ := brokerProps["HOST_ENDPOINT"].(string)
				return &item, hostEndpoint, nil
			}
		}
	}

	return nil, "", fmt.Errorf("no matching Akri instance found for UUID: %s", uuid)
}

func (r *FlashJobReconciler) handleFlashingPod(ctx context.Context, flashJob *flashv1alpha1.FlashJob, hostEndpoint string, akriInstance *unstructured.Unstructured) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// check if the service exist
	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-service", Namespace: flashJob.Namespace}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// create service
			svc, err = r.createService(ctx, flashJob)
			if err != nil {
				logger.Error(err, "Failed to create Service")
				flashJob.Status.Phase = "Failed"
				flashJob.Status.Message = "Failed to create service"
				r.Status().Update(ctx, flashJob)
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Wait for External IP
	logger.Info("Waiting for External IP of Service")
	externalIP, err := r.waitForServiceIP(ctx, flashJob)
	if err != nil {
		logger.Error(err, "Timeout waiting for Service IP")
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Timeout waiting for service IP"
		r.Status().Update(ctx, flashJob)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	logger.Info("Service External IP acquired", "ExternalIP", externalIP)

	// Update FlashJob with external IP
	flashJob.Spec.ExternalIP = externalIP
	if err := r.Update(ctx, flashJob); err != nil {
		logger.Error(err, "Failed to update FlashJob with external IP")
		return ctrl.Result{}, err
	}

	foundPod := &corev1.Pod{}
	err = r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-flashing-pod", Namespace: flashJob.Namespace}, foundPod)

	if err != nil && errors.IsNotFound(err) {
		// Create new pod
		pod := r.createFlashPod(flashJob, hostEndpoint, akriInstance, externalIP)
		if err := r.Create(ctx, pod); err != nil {
			logger.Error(err, "Failed to create flashing Pod")
			flashJob.Status.Phase = "Failed"
			flashJob.Status.Message = "Failed to create flashing pod"
			r.Status().Update(ctx, flashJob)
			return ctrl.Result{}, err
		}
		flashJob.Status.Phase = "Running"
		flashJob.Status.Message = "Created flashing pod"
		r.Status().Update(ctx, flashJob)
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Update status based on pod phase
	return r.updateFlashJobStatus(ctx, flashJob, foundPod)
}

func (r *FlashJobReconciler) createFlashPod(flashJob *flashv1alpha1.FlashJob, hostEndpoint string, akriInstance *unstructured.Unstructured, externalIP string) *corev1.Pod {
	brokerProps, _, _ := unstructured.NestedMap(akriInstance.Object, "spec", "brokerProperties")
	device, _ := brokerProps["DEVICE"].(string)
	applicationType, _ := brokerProps["NEW_TYPE"].(string)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      flashJob.Name + "-flashing-pod",
			Namespace: flashJob.Namespace,
			Labels: map[string]string{
				"app": flashJob.Name,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "flash-container",
				Image: flashJob.Spec.FlashjobPodImage,
				Env: []corev1.EnvVar{
					{Name: "FIRMWARE", Value: flashJob.Spec.Firmware},
					{Name: "UUID", Value: flashJob.Spec.UUID},
					{Name: "HOST_ENDPOINT", Value: hostEndpoint},
					{Name: "DEVICE", Value: device},
					{Name: "APPLICATION_TYPE", Value: applicationType},
					{Name: "VERSION", Value: flashJob.Spec.Version},
					{Name: "EXTERNAL_IP", Value: flashJob.Spec.ExternalIP},
					//{Name: "FlashjobPodImage", Value: FlashjobPodImage},
				},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// create service
func (r *FlashJobReconciler) createService(ctx context.Context, flashJob *flashv1alpha1.FlashJob) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      flashJob.Name + "-service",
			Namespace: flashJob.Namespace,
			Labels: map[string]string{
				"app": flashJob.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{
					Port:       4433,
					TargetPort: intstr.FromInt(4433),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app": flashJob.Name,
			},
		},
	}

	err := r.Create(ctx, svc)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// waiting  for ip
func (r *FlashJobReconciler) waitForServiceIP(ctx context.Context, flashJob *flashv1alpha1.FlashJob) (string, error) {
	var svc corev1.Service
	for {
		err := r.Get(ctx, client.ObjectKey{
			Namespace: flashJob.Namespace,
			Name:      flashJob.Name + "-service",
		}, &svc)
		if err != nil {
			return "", err
		}

		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			if svc.Status.LoadBalancer.Ingress[0].IP != "" {
				return svc.Status.LoadBalancer.Ingress[0].IP, nil
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for service IP")
		case <-time.After(5 * time.Second):
			continue
		}
	}
}

func (r *FlashJobReconciler) updateFlashJobStatus(ctx context.Context, flashJob *flashv1alpha1.FlashJob, pod *corev1.Pod) (ctrl.Result, error) {
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		flashJob.Status.Phase = "Completed"
		flashJob.Status.Message = "Firmware flashing completed successfully"
	case corev1.PodFailed:
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Firmware flashing failed"
	default:
		flashJob.Status.Phase = "InProgress"
		flashJob.Status.Message = "Firmware flashing in progress"
	}

	if err := r.Status().Update(ctx, flashJob); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *FlashJobReconciler) handleDeletion(ctx context.Context, flashJob *flashv1alpha1.FlashJob) (ctrl.Result, error) {
	if containsString(flashJob.GetFinalizers(), flashJobFinalizer) {
		// Delete Pod
		if err := r.deleteFlashingPod(ctx, flashJob); err != nil {
			return ctrl.Result{}, err
		}

		// Delete Service
		svc := &corev1.Service{}
		err := r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-service", Namespace: flashJob.Namespace}, svc)
		if err == nil {
			if err := r.Delete(ctx, svc); err != nil {
				return ctrl.Result{}, err
			}
		}

		flashJob.SetFinalizers(removeString(flashJob.GetFinalizers(), flashJobFinalizer))
		if err := r.Update(ctx, flashJob); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *FlashJobReconciler) deleteFlashingPod(ctx context.Context, flashJob *flashv1alpha1.FlashJob) error {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: flashJob.Name + "-flashing-pod", Namespace: flashJob.Namespace}, pod)
	if err == nil {
		return r.Delete(ctx, pod)
	}
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *FlashJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flashv1alpha1.FlashJob{}).
		Owns(&corev1.Pod{}).
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
