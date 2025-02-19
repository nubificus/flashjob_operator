# Components Overview

The **FlashJob Operator** is a Kubernetes operator designed to manage the lifecycle of FlashJob custom resources. It automates the process of flashing firmware to devices using Akri instances and manages associated Kubernetes resources (Pods, Services, etc.).

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

## 1. Custom Resource Definition (CRD): 

##### File: config/crd/bases/application.flashjob.nbfc.io_flashjobs.yaml

The FlashJob CRD defines the schema for the custom resource that the operator manages. It includes the following fields:

### Spec
- **UUID**: Unique identifier for the device.
- **Firmware**: The firmware image to be flashed.
- **Version**: The version of the firmware.
- **HostEndpoint**: The endpoint of the host where the device is connected.
- **Device**: The type of device (e.g., `esp32s2`).
- **ApplicationType**: The type of application (e.g., `thermo`).
- **ExternalIP**: The external IP address of the service.
- **FlashjobPodImage**: The container image used from the  flash pod.

### Status
- **Conditions**: A list of conditions representing the current state of the FlashJob.
- **Message**: A human-readable message describing the current state.
- **HostEndpoint**: The endpoint of the host where the device is connected.

## 2. FlashJob Controller:

#### File: internal/controller/flashjob_controller.go

The controller manages the lifecycle of FlashJob resources. It handles the creation, updating, and deletion of resources, including the creation of flashing pods and services.

### Key Functions:

- **Reconcile:** 
    - The main reconciliation loop that handles the creation and updating of FlashJob resources.

    - Ensures that the desired state (defined in the FlashJobSpec) matches the observed state.

    - Manages the lifecycle of associated Pods and Services.

- **getAkriInstanceDetails:** 
    - Retrieves details about the Akri instance associated with the device UUID.

- **handleFlashingPod:** 
    - Manages the creation and updating of the flashing Pod.
    - Creates a Pod using the FlashjobPodImage and configures it with environment variables such as FIRMWARE, UUID, HOST_ENDPOINT, and DEVICE.

- **createService:** 
    - Creates a Kubernetes Service of type LoadBalancer to expose the flashing Pod.

- **waitForServiceIP:** 
    - Waits for the Service to acquire an external IP.

    - Updates the FlashJob resource with the external IP once it is available.

- **updateFlashJobStatus:** 
    - Updates the status of the FlashJob resource based on the pod's phase.

- **handleDeletion:** 
    - Handles the deletion of FlashJob resources.

    - Cleans up associated resources such as Pods and Services.

    - Removes the finalizer to allow the resource to be deleted from the cluster.

## 3. Example FlashJob Resource:
#### File: config/samples/application_v1alpha1_flashjob.yaml

Below is an example of a FlashJob resource:
```
apiVersion: application.flashjob.nbfc.io/v1alpha1
kind: FlashJob
metadata:
  name: flashjob
  namespace: default
spec:
  uuid: 2559ad7a-543d-41d2-b285-82eab2a8589a
  device: esp32
  hostEndpoint: 
  firmware: harbor.nbfc.io/nubificus/esp32:x.x.xxxxxxx
  version: 0.2.0
  applicationType: thermo
  externalIP:
  flashjobPodImage: harbor.nbfc.io/nubificus/iot/esp32-sota:xxx.x-debug
```

### Fields 

- **uuid:** The unique identifier for the device.

- **device:** The type of device (e.g., esp32).

- **firmware:** The firmware image to be flashed.

- **version:** The version of the firmware.

- **applicationType:** The type of application (e.g., thermo).

- **flashjobPodImage:** The container image used from the pod.

 ## 4. FlashJob Types (Go Structs)

 #### File: api/v1alpha1/flashjob_types.go

 This file defines the Go types for the FlashJob custom resource. These types are used by the controller to interact with FlashJob resources.

### FlashJobSpec

Defines the desired state of a FlashJob.
```
type FlashJobSpec struct {
    UUID             string  `json:"uuid"`
    Firmware         string  `json:"firmware"`
    Version          string  `json:"version"`
    HostEndpoint     *string `json:"hostEndpoint,omitempty"`
    Device           string  `json:"device,omitempty"`
    ApplicationType  string  `json:"applicationType"`
    ExternalIP       string  `json:"externalIP,omitempty"`
    FlashjobPodImage string  `json:"flashjobPodImage,omitempty"`
}
```

### FlashJobStatus

Defines the observed state of a FlashJob.

```
type FlashJobStatus struct {
    Conditions   []metav1.Condition `json:"conditions,omitempty"`
    Message      string             `json:"message,omitempty"`
    HostEndpoint string             `json:"hostEndpoint,omitempty"`
    Phase        string             `json:"phase,omitempty"`
}
```


## How It Works

 ### 1) Reconciliation Loop:

- The controller watches for changes to FlashJob resources.

- When a FlashJob is created or updated, the Reconcile function is triggered.

- The controller ensures that the required resources (Pods, Services) are created and that the FlashJob status is updated accordingly.

### 2) Firmware Flashing:

- The controller creates a Pod with the specified FlashjobPodImage to perform the firmware upgrade.

- The Pod is configured with environment variables like FIRMWARE, UUID, HOST_ENDPOINT, and DEVICE.

### 3) Service Management:

- A Service of type LoadBalancer is created to expose the flashing Pod.

- The controller waits for the Service to acquire an external IP and updates the FlashJob with this IP.

## Debugging and Testing

1) ### Install the CRDs into the Kubernetes cluster:
    Run the following command to install the Custom Resource Definitions (CRDs):
    ```
    make install
    ```

2) ### Run the controller for debugging:
    To test and debug your controller with immediate feedback, run the following command:
    ```
    make run
    ```



## Deploying the Controller to a Kubernetes Cluster


Below are two methods for deploying the controller:

1. ### Direct Deployment Using **make deploy**

This method allows us to build, push, and deploy flash controller image directly to the cluster.

- **Build and Push the Controller Image**

Run the following command, replacing **some-registry project-name:tag** with the appropriate registry, project name, and tag:

```
make docker-build docker-push IMG=<some-registry>/<project-name>:tag
```

- **Deploy the Controller to the Cluster**

After the image is successfully built and pushed, deploy the controller with:
```
make deploy IMG=<some-registry>/<project-name>:tag
```

- **Apply the Custom Resource (CR)**

Once the controller is deployed, apply Custom Resource file:
```
kubectl apply -f config/samples/application_v1alpha1_flashjob.yaml
```

> **_NOTE:_** The current image for the Flash controller is : panosmavrikos/akri_operator:1.12.0


2. ### Deployment Using an Installer YAML

This approach packages all necessary controller resources into a single installation YAML file for reuse.

- **Build and Push the Controller Image**
```
make docker-build docker-push IMG=<some-registry>/<project-name>:tag
```
- **Generate the Installer YAML**

Use the following command to create the install.yaml, which includes the Custom Resource Definitions (CRDs), controller, and other resources:   

```
make build-installer IMG=<some-registry>/<project-name>:tag
```

> **_NOTE:_** The current image for the Flash controller is : panosmavrikos/akri_operator:1.12.0

- **Apply the Installer YAML to the Cluster**
```
kubectl apply -f dist/install.yaml
```
- **Apply the Custom Resource File**
```
kubectl apply -f config/samples/application_v1alpha1_flashjob.yaml
```

