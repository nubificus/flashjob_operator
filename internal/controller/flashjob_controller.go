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
	//      "strings"
	//      "sigs.k8s.io/controller-runtime/pkg/controller"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type FlashJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//const flashJobFinalizer = "flashjob.finalizers.flashjob.nbfc.io"

const flashJobFinalizer = "application.flashjob.nbfc.io/flashjob-finalizer"

// +kubebuilder:rbac:groups=application.flashjob.nbfc.io,resources=flashjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=application.flashjob.nbfc.io,resources=flashjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=application.flashjob.nbfc.io,resources=flashjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=akri.sh,resources=instances,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

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

	// Get Akri instance details for the specified UUIDs
	akriInstances, hostEndpoints, err := r.getAkriInstanceDetails(ctx, flashJob.Spec.UUID)
	if err != nil {
		logger.Error(err, "Failed to get Akri instance details")
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Failed to get device details"
		r.Status().Update(ctx, &flashJob)
		return ctrl.Result{}, err
	}

	// Create or update flashing pods for each Akri instance
	anyInProgress := false
	for i, akriInstance := range akriInstances {
		uuid := akriInstance.GetUID()
		if containsString(flashJob.Status.CompletedUUIDs, string(uuid)) {
			continue
		}
		result, err := r.handleFlashingPod(ctx, &flashJob, hostEndpoints[i], akriInstance)
		if err != nil {
			logger.Error(err, "Failed to handle flashing pod", "uuid", uuid)
			continue
		}
		if result.Requeue || result.RequeueAfter > 0 {
			anyInProgress = true
		}
	}

	if !anyInProgress {
		flashJob.Status.Phase = "Completed"
		flashJob.Status.Message = "All flashing pods completed successfully"
		if err := r.Status().Update(ctx, &flashJob); err != nil {
			logger.Error(err, "Failed to update FlashJob status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *FlashJobReconciler) getAkriInstanceDetails(ctx context.Context, uuids []string) ([]*unstructured.Unstructured, []string, error) {
	akriList := &unstructured.UnstructuredList{}
	akriList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "akri.sh",
		Version: "v0",
		Kind:    "Instance",
	})

	if err := r.List(ctx, akriList, &client.ListOptions{}); err != nil {
		return nil, nil, err
	}

	var instances []*unstructured.Unstructured
	var endpoints []string

	for _, item := range akriList.Items {
		itemUID, _, _ := unstructured.NestedString(item.Object, "metadata", "uid")

		for _, uuid := range uuids {
			if itemUID == uuid {
				brokerProps, exists, _ := unstructured.NestedMap(item.Object, "spec", "brokerProperties")
				if exists {
					if hostEndpoint, ok := brokerProps["HOST_ENDPOINT"].(string); ok {
						instances = append(instances, &item)
						endpoints = append(endpoints, hostEndpoint)
					}
				}
			}
		}
	}

	if len(instances) == 0 {
		return nil, nil, fmt.Errorf("no matching Akri instances found for given UUIDs")
	}

	return instances, endpoints, nil
}

func (r *FlashJobReconciler) handleFlashingPod(ctx context.Context, flashJob *flashv1alpha1.FlashJob, hostEndpoint string, akriInstance *unstructured.Unstructured) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	uuidString := string(akriInstance.GetUID())
	svcName := fmt.Sprintf("%s-service-%s", flashJob.Name, uuidString)

	// Check if the Service exists
	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: svcName, Namespace: flashJob.Namespace}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the Service
			svc, err = r.createService(ctx, flashJob, uuidString)
			if err != nil {
				logger.Error(err, "Failed to create Service")
				flashJob.Status.Phase = "Failed"
				flashJob.Status.Message = "Failed to create service"
				r.Status().Update(ctx, flashJob)
				return ctrl.Result{}, err
			}
			logger.Info("Service created successfully", "service", svc.Name)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to fetch Service")
		return ctrl.Result{}, err
	}

	// Wait for Service External IP
	externalIP, err := r.waitForServiceIP(ctx, flashJob.Namespace, svcName)
	if err != nil {
		logger.Error(err, "Timeout waiting for Service IP")
		flashJob.Status.Phase = "Failed"
		flashJob.Status.Message = "Timeout waiting for service IP"
		r.Status().Update(ctx, flashJob)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Proceed to create Pod with the obtained externalIP
	podName := fmt.Sprintf("%s-flashing-pod-%s", flashJob.Name, uuidString)
	foundPod := &corev1.Pod{}
	err = r.Get(ctx, client.ObjectKey{Name: podName, Namespace: flashJob.Namespace}, foundPod)
	if err != nil && errors.IsNotFound(err) {
		pod := r.createFlashPod(flashJob, hostEndpoint, akriInstance, externalIP)
		if err = r.Create(ctx, pod); err != nil {
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

	return r.updateFlashJobStatus(ctx, flashJob, foundPod)
}

func (r *FlashJobReconciler) createFlashPod(flashJob *flashv1alpha1.FlashJob, hostEndpoint string, akriInstance *unstructured.Unstructured, externalIP string) *corev1.Pod {
	brokerProps, _, _ := unstructured.NestedMap(akriInstance.Object, "spec", "brokerProperties")
	device, _ := brokerProps["DEVICE"].(string)
	//applicationType, _ := brokerProps["NEW_TYPE"].(string)
	applicationType, _ := brokerProps["APPLICATION_TYPE"].(string)

	// Use the specific UUID for this Pod
	uuidString := string(akriInstance.GetUID())

	podName := fmt.Sprintf("%s-flashing-pod-%s", flashJob.Name, uuidString)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: flashJob.Namespace,
			Labels: map[string]string{
				"app":  flashJob.Name,
				"uuid": uuidString,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: flashJob.APIVersion,
					Kind:       flashJob.Kind,
					Name:       flashJob.Name,
					UID:        flashJob.UID,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "flash-container",
				Image: flashJob.Spec.FlashjobPodImage,
				Env: []corev1.EnvVar{
					{Name: "FIRMWARE", Value: flashJob.Spec.Firmware},
					{Name: "UUID", Value: uuidString},
					{Name: "HOST_ENDPOINT", Value: hostEndpoint},
					{Name: "DEVICE", Value: device},
					{Name: "APPLICATION_TYPE", Value: applicationType},
					{Name: "VERSION", Value: flashJob.Spec.Version},
					{Name: "EXTERNAL_IP", Value: externalIP},
					//{Name: "FlashjobPodImage", Value: FlashjobPodImage},
				},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// create service
func (r *FlashJobReconciler) createService(ctx context.Context, flashJob *flashv1alpha1.FlashJob, uuid string) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service-%s", flashJob.Name, uuid),
			Namespace: flashJob.Namespace,
			Labels: map[string]string{
				"app":  flashJob.Name,
				"uuid": uuid,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: flashJob.APIVersion,
					Kind:       flashJob.Kind,
					Name:       flashJob.Name,
					UID:        flashJob.UID,
				},
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
				"app":  flashJob.Name,
				"uuid": uuid,
			},
		},
	}

	if err := r.Create(ctx, svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// waiting  for ip
func (r *FlashJobReconciler) waitForServiceIP(ctx context.Context, namespace, serviceName string) (string, error) {
	var svc corev1.Service
	for {
		err := r.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      serviceName,
		}, &svc)
		if err != nil {
			return "", err
		}

		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			if ip := svc.Status.LoadBalancer.Ingress[0].IP; ip != "" {
				return ip, nil
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for service IP")
		case <-time.After(5 * time.Second):
			// Retry
		}
	}
}

func (r *FlashJobReconciler) updateFlashJobStatus(ctx context.Context, flashJob *flashv1alpha1.FlashJob, pod *corev1.Pod) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		flashJob.Status.Phase = "Completed"
		flashJob.Status.Message = "Firmware flashing completed successfully"
		uuidString := pod.Labels["uuid"]
		// Add UUID to CompletedUUIDs
		if !containsString(flashJob.Status.CompletedUUIDs, uuidString) {
			flashJob.Status.CompletedUUIDs = append(flashJob.Status.CompletedUUIDs, uuidString)
		}
		// Delete Pod and Service
		if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete completed Pod", "pod", pod.Name)
			return ctrl.Result{}, err
		}

		svcName := fmt.Sprintf("%s-service-%s", flashJob.Name, uuidString)
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: flashJob.Namespace,
			},
		}
		if err := r.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete Service", "service", svcName)
			return ctrl.Result{}, err
		}
		logger.Info("Deleted completed Pod and Service", "pod", pod.Name, "service", svcName)
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
	//return ctrl.Result{}, nil
	return ctrl.Result{Requeue: true}, nil

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

		//if err := r.Patch(ctx, flashJob, client.Merge); err != nil {
		//      return ctrl.Result{}, err
		//}
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
		Owns(&corev1.Service{}).
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
