import yaml
from kubernetes import client, config
import subprocess
import time

def get_akri_instances():
    """Fetches Akri Instances from the Kubernetes API Server."""
    try:
        config.load_kube_config()
        api = client.CustomObjectsApi()

        akri_instances = api.list_namespaced_custom_object(
            group="akri.sh",
            version="v0",
            namespace="default",
            plural="instances",
        )
        return akri_instances
    except Exception as e:
        print("Error accessing Kubernetes API:", e)
        return None

def filter_instances(instances, uuid=None, device_type=None, application_type=None):
    """Filters instances based on UUID, Device Type, and Application Type."""
    filtered = []
    for item in instances.get("items", []):
        instance_uuid = item["metadata"]["uid"]
        device = item["spec"]["brokerProperties"].get("DEVICE", "Unknown Device Type")
        app_type = item["spec"]["brokerProperties"].get("APPLICATION_TYPE", "Unknown Application Type")

        if (uuid and uuid != instance_uuid) or \
           (device_type and device_type.lower() != device.lower()) or \
           (application_type and application_type.lower() != app_type.lower()):
            continue

        filtered.append({
            "uuid": instance_uuid,
            "deviceType": device,
            "applicationType": app_type
        })

    return filtered

def save_uuids_to_yaml(uuids, firmware, flashjob_pod_image, filename="config/samples/application_v1alpha1_flashjob.yaml"):
    """Saves the selected UUIDs into the specified YAML file for the FlashJob template."""
    if uuids:
        flashjob_data = {
            "apiVersion": "application.flashjob.nbfc.io/v1alpha1",
            "kind": "FlashJob",
            "metadata": {
                "name": "flashjob",
                "namespace": "default"
            },
            "spec": {
                "applicationType": None,
                "device": None,
                "externalIP": None,
                "firmware": firmware,
                "flashjobPodImage": flashjob_pod_image,
                "hostEndpoint": None,
                "uuid": uuids,
                "version": "0.2.0"
            }
        }

        with open(filename, 'w') as file:
            yaml.safe_dump(flashjob_data, file)

        print(f"UUIDs saved to {filename}")
    else:
        print("No UUIDs selected, nothing to save.")

def user_select_instances(instances):
    """Allows user to select all or individual instances interactively."""
    if not instances:
        print("No instances found matching the criteria.")
        return []

    print("\nAvailable Akri Instances:")
    for i, inst in enumerate(instances):
        print(f"[{i}] UUID: {inst['uuid']}, Device Type: {inst['deviceType']}, Application Type: {inst['applicationType']}")

    select_all = input("\nDo you want to select all UUIDs? (y/n): ").lower()
    if select_all == "y":
        return [inst["uuid"] for inst in instances]

    selected_indices = input("\nEnter the numbers of the UUIDs you want to include (comma-separated): ")
    selected_uuids = [
        instances[int(i)]["uuid"]
        for i in selected_indices.split(",")
        if i.isdigit()
    ]
    return selected_uuids

def apply_yaml(filename):
    """Applies the YAML file using kubectl."""
    try:
        subprocess.run(["kubectl", "apply", "-f", filename], check=True)
        print(f"Successfully applied {filename}")
    except subprocess.CalledProcessError as e:
        print(f"Failed to apply {filename}: {e}")

def gradual_rollout(uuids, firmware, flashjob_pod_image, step=5, delay=60):
    """Applies the YAML file in batches for gradual roll-out."""
    for i in range(0, len(uuids), step):
        batch = uuids[i:i + step]
        print(f"\nApplying batch {i//step + 1} with UUIDs: {batch}")

        save_uuids_to_yaml(batch, firmware, flashjob_pod_image)
        apply_yaml("config/samples/application_v1alpha1_flashjob.yaml")

        if i + step < len(uuids):
            print(f"Waiting for {delay} seconds before the next batch...")
            time.sleep(delay)

if __name__ == "__main__":
    akri_data = get_akri_instances()
    if akri_data:
        device_type_filter = input("Enter device type to filter (or press Enter to skip): ") or None
        application_type_filter = input("Enter application type to filter (or press Enter to skip): ") or None
        uuid_filter = input("Enter UUID to filter (or press Enter to skip): ") or None

        filtered_instances = filter_instances(
            akri_data,
            uuid_filter,
            device_type_filter,
            application_type_filter
        )
        selected_instances = user_select_instances(filtered_instances)

        if selected_instances:
            firmware = input("Enter the firmware to use: ")
            flashjob_pod_image = input("Enter the flashjobPodImage to use (e.g. harbor.nbfc.io/nubificus/iot/esp32-flashjob:local): ")
            step = int(input("Enter the number of UUIDs per batch (default is 5): ") or 5)
            delay = int(input("Enter the delay between batches in seconds (default is 60): ") or 60)

            gradual_rollout(selected_instances, firmware, flashjob_pod_image, step, delay)
        else:
            print("No matching instances found.")
    else:
        print("Failed to retrieve Akri instances.")
