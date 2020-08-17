package device

import (
	"context"
	"time"

	"github.com/hashicorp/nomad/plugins/device"
	"github.com/hashicorp/nomad/plugins/shared/structs"

	aravis "github.com/Standard-Cognition/genicam-device-plugin/aravis"
)

// doFingerprint is the long-running goroutine that detects device changes
func (d *GenicamDevice) doFingerprint(ctx context.Context, devices chan *device.FingerprintResponse) {
	defer close(devices)

	// Create a timer that will fire immediately for the first detection
	ticker := time.NewTimer(0)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticker.Reset(d.fingerprintPeriod)
		}

		d.writeFingerprintToChannel(devices)
	}
}

// fingerprintedDevice is what we "discover" and transform into device.Device objects.
//
// plugin implementations will likely have a native struct provided by the corresonding SDK
type fingerprintedDevice struct {
    device_id string
    physical_id string
    model string
    serial_nbr string
    vendor string
    address string
    protocol string
}

// writeFingerprintToChannel collects fingerprint info, partitions devices into
// device groups, and sends the data over the provided channel.
func (d *GenicamDevice) writeFingerprintToChannel(devices chan<- *device.FingerprintResponse) {
    d.deviceLock.Lock()
    defer d.deviceLock.Unlock()

    aravis.UpdateDeviceList()
    devs, err := aravis.GetDevices()
    if err != nil {
        d.logger.Error("failed to get devices", "error", err)
    }

    var discoveredDevices []*fingerprintedDevice

    //// "discover" some devices
    for _, dev := range devs {
        device_id, err := dev.Id()
        if err != nil {
            d.logger.Error("failed to get device id", "error", err)
            continue
        }

        physical_id, err := dev.PhysicalId()
        if err != nil {
            d.logger.Error("failed to get device physical id", "error", err)
            continue
        }

        model, err := dev.Model()
        if err != nil {
            d.logger.Error("failed to get device model", "error", err)
            continue
        }

        serial_nbr, err := dev.SerialNbr()
        if err != nil {
            d.logger.Error("failed to get device serial number", "error", err)
            continue
        }

        vendor, err := dev.Vendor()
        if err != nil {
            d.logger.Error("failed to get device vendor", "error", err)
            continue
        }

        address, err := dev.Address()
        if err != nil {
            d.logger.Error("failed to get device address", "error", err)
            continue
        }

        protocol, err := dev.Protocol()
        if err != nil {
            d.logger.Error("failed to get device protocol", "error", err)
            continue
        }

        d.logger.Debug("found device", "device_id", device_id)

        discoveredDevices = append(discoveredDevices, &fingerprintedDevice {
            device_id: device_id,
            physical_id: physical_id,
            model: model,
            serial_nbr: serial_nbr,
            vendor: vendor,
            address: address,
            protocol: protocol,
        })
    }

    // during fingerprinting, devices are grouped by "device group" in
    // order to facilitate scheduling
    // devices in the same device group should have the same
    // Vendor, Type, and Name ("Model")
    // Build Fingerprint response with computed groups and send it over the channel
    deviceListByDeviceName := make(map[string][]*fingerprintedDevice)
    for _, device := range discoveredDevices {
        deviceListByDeviceName[device.model] = append(deviceListByDeviceName[device.model], device)
        d.devices[device.serial_nbr] = device.address
    }

    // Build Fingerprint response with computed groups and send it over the channel
    deviceGroups := make([]*device.DeviceGroup, 0, len(deviceListByDeviceName))
    for groupName, devices := range deviceListByDeviceName {
        deviceGroups = append(deviceGroups, deviceGroupFromFingerprintData(groupName, devices))
    }

    devices <- device.NewFingerprint(deviceGroups...)
}

// deviceGroupFromFingerprintData composes deviceGroup from a slice of detected devicers
func deviceGroupFromFingerprintData(groupName string, deviceList []*fingerprintedDevice) *device.DeviceGroup {
	// deviceGroup without devices makes no sense -> return nil when no devices are provided
	if len(deviceList) == 0 {
		return nil
	}

	devices := make([]*device.Device, 0, len(deviceList))
	for _, dev := range deviceList {
		devices = append(devices, &device.Device{
			ID: dev.serial_nbr,
            Healthy: true,  // TODO: this could probably be useful
            HwLocality: nil,  // TODO: is this useful for anything WRT aravis/tis/genicam?
		})
	}

	return &device.DeviceGroup{
		Vendor: vendor,
		Type: deviceType,
        Name: groupName,
		Devices: devices,
		// The device API assumes that devices with the same DeviceName have the same
		// attributes like amount of memory, power, bar1memory, etc.
		// If not, then they'll need to be split into different device groups
		// with different names.
		Attributes: map[string]*structs.Attribute{},
	}
}
