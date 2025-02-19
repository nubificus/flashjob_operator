package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	//nolint:golint
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	applicationv1alpha1 "flashjob/api/v1alpha1"
)

const typeAvailableFlashJob = "Available" // Προσθήκη της σταθεράς

var _ = Describe("FlashJob controller", func() {
	Context("FlashJob controller test", func() {

		const FlashJobName = "test-flashjob"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      FlashJobName,
				Namespace: FlashJobName,
			},
		}

		typeNamespaceName := types.NamespacedName{
			Name:      FlashJobName,
			Namespace: FlashJobName,
		}
		flashjob := &applicationv1alpha1.FlashJob{}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Setting the Image ENV VAR which stores the Operand image")
			err = os.Setenv("FLASHJOB_IMAGE", "example.com/image:test")
			Expect(err).To(Not(HaveOccurred()))

			By("creating the custom resource for the Kind FlashJob")
			err = k8sClient.Get(ctx, typeNamespaceName, flashjob)
			if err != nil && errors.IsNotFound(err) {
				flashjob := &applicationv1alpha1.FlashJob{
					ObjectMeta: metav1.ObjectMeta{
						Name:      FlashJobName,
						Namespace: namespace.Name,
					},
					Spec: applicationv1alpha1.FlashJobSpec{
						UUID:     []string{"example-uuid"},
						Firmware: "example-firmware",
						Version:  "v1.0",
					},
				}

				err = k8sClient.Create(ctx, flashjob)
				Expect(err).To(Not(HaveOccurred()))
			}
		})

		AfterEach(func() {
			By("Removing the custom resource for the Kind FlashJob")
			found := &applicationv1alpha1.FlashJob{}
			err := k8sClient.Get(ctx, typeNamespaceName, found)
			Expect(err).To(Not(HaveOccurred()))

			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), found)
			}, 2*time.Minute, time.Second).Should(Succeed())

			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)

			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("FLASHJOB_IMAGE")
		})

		It("should successfully reconcile a custom resource for FlashJob", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &applicationv1alpha1.FlashJob{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			flashjobReconciler := &FlashJobReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := flashjobReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the FlashJob instance")
			Eventually(func() error {
				if flashjob.Status.Conditions != nil &&
					len(flashjob.Status.Conditions) != 0 {
					latestStatusCondition := flashjob.Status.Conditions[len(flashjob.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{
						Type:   typeAvailableFlashJob,
						Status: metav1.ConditionTrue,
						Reason: "Reconciling",
						Message: fmt.Sprintf(
							"Deployment for custom resource (%s) created successfully",
							flashjob.Name),
					}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the FlashJob instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
