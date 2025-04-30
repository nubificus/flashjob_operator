# FlashJob Operator

The **FlashJob Operator** is a Kubernetes operator designed to manage the lifecycle of FlashJob custom resources. It automates the process of flashing firmware to devices using Akri instances and manages associated Kubernetes resources  such as Pods and Services.

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- Kubebuilder (for development and scaffolding)
- Python 3.x (for filter_uuid.py): With kubernetes and pyyaml packages

## Create Project from begin

### Install Kubebuilder
To install Kubebuilder, run the following commands:
```
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
chmod +x kubebuilder && sudo mv kubebuilder /usr/local/bin/
```
### Project Setup
1. Set up Go environment and create a project directory:
```
export GOPATH=$PWD
mkdir -p $GOPATH/flashjob_operator
cd $GOPATH/flashjob_operator
go mod init flashjob
```
2. Initialize the project with Kubebuilder:
```
kubebuilder init --domain flashjob.nbfc.io
```
3. Create the API for the FlashJob resource:
```
kubebuilder create api --group application --version v1alpha1 --kind FlashJob --image=ubuntu:latest --image-container-command="sleep,infinity" --image-container-port="22" --run-as-user="1001" --plugins="deploy-image/v1-alpha" --make=false
```
4. Generate the CRDs and install them into the cluster:
```
make manifests
make install
```

## Components Overview

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
- **Phase**: Current phase (InProgress, Completed, Failed).
- **CompletedUUIDs**:List of UUIDs for devices that have completed flashing.

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
    - Creates a LoadBalancer Service per Pod with a unique external IP.

- **waitForServiceIP:**
    - Waits for the Service to acquire an external IP.

    - Updates the FlashJob resource with the external IP once it is available.

- **updateFlashJobStatus:**
    - Updates the FlashJob status based on Pod phase and deletes completed resources.

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
  uuid:
    - d95c7c2d-7dc5-427b-9bf7-51a5d299c99e
    - 44606d10-7c29-4098-9d6a-dc16f95090d1
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
    UUID             []string `json:"uuid"`
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
    CompletedUUIDs []string           `json:"completedUUIDs,omitempty"`
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

- Tracks Pod status (Running, Succeeded, Failed) and updates the FlashJob status.

### 3) Service Management:

- A Service of type LoadBalancer is created to expose the flashing Pod.

- The controller waits for the Service to acquire an external IP and updates the FlashJob with this IP.

- Deletes Services when Pods complete or during cleanup.

### 4) Resource Cleanup:

- Deletes completed Pods and their associated Services.

- Cleans up all resources when a FlashJob is deleted, removing the finalizer.

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

> **_NOTE:_** The current image for the Flash controller is : panosmavrikos/akri_operator:v1.16.0


2. ### One click installation 

- **Apply the Installer YAML to the Cluster**
```
kubectl apply -f https://raw.githubusercontent.com/nubificus/flashjob_operator/uuid_array/dist/install.yaml
```
- **Apply the Custom Resource File or use Python Script: filter_uuid.py**
```
kubectl apply -f config/samples/application_v1alpha1_flashjob.yaml
```
> **_NOTE:_** The current image for the Flash controller is : panosmavrikos/akri_operator:v1.16.0



#  Python Script: filter_uuid.py
The **filter_uuid.py** script is a utility designed to interact with Akri instances in a Kubernetes cluster. It allows users to filter Akri instances based on UUID, device type, and application type, and then save the selected UUIDs into a YAML file for use with the FlashJob operator. Additionally, it supports gradual roll-out of firmware updates to selected devices.

### Prerequisites
- Python 3.x
- kubernetes Python package (pip install kubernetes)
- pyyaml Python package (pip install pyyaml)
- Access to a Kubernetes cluster with Akri instances configured.

###  Script Overview

1) **Fetch Akri Instances:**  Retrieves Akri instances from the Kubernetes API server.

2) **Filter Instances:**  Filters instances based on UUID, device type, and application type.

3) **User Selection:**  Allows the user to select specific instances or all instances.

4) **Save UUIDs to YAML:** Saves the selected UUIDs into a YAML file for use with the FlashJob operator.

5) **Gradual Rollout:**  Applies the YAML file in batches for gradual firmware roll-out.

### Usage

1) **Run the Script:**

```
python3 filter_uuid.py
```
2) **Filter Instances:**

- The script will prompt to enter filters for device type, application type, and UUID.

3) **Select Instances:**

- After filtering, the script will display the available Akri instances and the use cat select all or specific instances.

4) **Enter Firmware Details:**

- Provide the firmware to be used for the selected devices.

5) **Gradual Rollout:**

- Specify the number of UUIDs per batch and the delay between batches (in seconds).

### Example Workflow
```
$ python3 filter_uuid.py
Enter device type to filter (or press Enter to skip): esp32
Enter application type to filter (or press Enter to skip): thermo
Enter UUID to filter (or press Enter to skip):

Available Akri Instances:
[0] UUID: d95c7c2d-7dc5-427b-9bf7-51a5d299c99e, Device Type: esp32, Application Type: thermo
[1] UUID: 44606d10-7c29-4098-9d6a-dc16f95090d1, Device Type: esp32, Application Type: thermo

Do you want to select all UUIDs? (y/n): y
Enter the firmware to use: harbor.nbfc.io/nubificus/esp32:x.x.xxxxxxx
Enter the number of UUIDs per batch (default is 5): 2
Enter the delay between batches in seconds (default is 60): 30

Applying batch 1 with UUIDs: ['d95c7c2d-7dc5-427b-9bf7-51a5d299c99e', '44606d10-7c29-4098-9d6a-dc16f95090d1']
UUIDs saved to flashjob_operator/config/samples/application_v1alpha1_flashjob.yaml
Successfully applied flashjob_operator/config/samples/application_v1alpha1_flashjob.yaml
Waiting for 30 seconds before the next batch...
```

### Script Functions

1) **get_akri_instances():** Fetches Akri instances from the Kubernetes API server.

2) **filter_instances():**  Filters instances based on UUID, device type, and application type.

3) **save_uuids_to_yaml():**  Saves the selected UUIDs into a YAML file.

4) **user_select_instances():**  Allows the user to select instances interactively.

5) **apply_yaml():** Applies the YAML file using kubectl.

6) **gradual_rollout():** Applies the YAML file in batches for gradual roll-out.


### Integration with FlashJob Operator

The script generates a YAML file (application_v1alpha1_flashjob.yaml) that can be directly used by the FlashJob operator to manage firmware updates for the selected devices. The YAML file includes the UUIDs of the selected devices, the firmware to be used, and other necessary configurations.

### Example YAML Output

```
apiVersion: application.flashjob.nbfc.io/v1alpha1
kind: FlashJob
metadata:
  name: flashjob
  namespace: default
spec:
  applicationType: thermo
  device: esp32
  externalIP:
  firmware: harbor.nbfc.io/nubificus/esp32:x.x.xxxxxxx
  flashjobPodImage: harbor.nbfc.io/nubificus/iot/x.x.xxxxxxx
  hostEndpoint:
  uuid:
    - d95c7c2d-7dc5-427b-9bf7-51a5d299c99e
    - 44606d10-7c29-4098-9d6a-dc16f95090d1
  version: "0.2.0"
```

> **_NOTE:_**
> - Ensure that the Kubernetes cluster is accessible and that the kubectl configuration is correctly set up.
> - The script assumes that the Akri instances are in the default namespace. Modify the script if your instances are in a different namespace.
