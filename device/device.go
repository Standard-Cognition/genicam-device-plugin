package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/device"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/kr/pretty"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// pluginName is the deviceName of the plugin
	// this is used for logging and (along with the version) for uniquely identifying
	// plugin binaries fingerprinted by the client
	pluginName = "genicam-device"

	// plugin version allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.0.1"

	// vendor is the label for the vendor providing the devices.
	// along with "type" and "model", this can be used when requesting devices:
	//   https://www.nomadproject.io/docs/job-specification/device.html#name
	vendor = "tis"

	// deviceType is the "type" of device being returned
	deviceType = "genicam"

    // environment variable names
    deviceSerialNbr = "GENICAM_DEVICE_SERIAL_NBR"
    deviceAddress = "GENICAM_DEVICE_ADDRESS"
)

var (
	// pluginInfo provides information used by Nomad to identify the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDevice,
		PluginApiVersions: []string{device.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// configSpec is the specification of the schema for this plugin's config.
	// this is used to validate the HCL for the plugin provided
	// as part of the client config:
	//   https://www.nomadproject.io/docs/configuration/plugin.html
	// options are here:
	//   https://github.com/hashicorp/nomad/blob/v0.10.0/plugins/shared/hclspec/hcl_spec.proto
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"enabled": hclspec.NewDefault(
			hclspec.NewAttr("enabled", "bool", false),
			hclspec.NewLiteral("true"),
		),
		"fingerprint_period": hclspec.NewDefault(
			hclspec.NewAttr("fingerprint_period", "string", false),
			hclspec.NewLiteral("\"5s\""),
		),
	})

    // error to return when a device is requested but the plugin isn't enabled
    errPluginDisabled = fmt.Errorf("genicam device is not enabled")
)

// Config contains configuration information for the plugin.
type Config struct {
    Enabled bool `codec:"enabled"`
	FingerprintPeriod string `codec:"fingerprint_period"`
}

// GenicamDevice contains a skeleton for most of the implementation of a
// device plugin.
type GenicamDevice struct {
	logger log.Logger

	// enabled indicates whether the plugin should be enabled
	enabled bool

	// fingerprintPeriod the period for the fingerprinting loop
	// most plugins that fingerprint in a polling loop will have this
	fingerprintPeriod time.Duration

	// devices is a list of fingerprinted devices
	// most plugins will maintain, at least, a list of the devices that were
	// discovered during fingerprinting.
	// we save the "device serial"/"ip address"
	devices    map[string]string
	deviceLock sync.RWMutex
}

// NewGenicamDevice returns a device plugin, used primarily by the main wrapper
//
// Plugin configuration isn't available yet, so there will typically be
// a limit to the initialization that can be performed at this point.
func NewGenicamDevice(log log.Logger) *GenicamDevice {
	return &GenicamDevice{
		logger:  log.Named(pluginName),
		devices: make(map[string]string),
	}
}

// PluginInfo returns information describing the plugin.
//
// This is called during Nomad client startup, while discovering and loading
// plugins.
func (d *GenicamDevice) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the configuration schema for the plugin.
//
// This is called during Nomad client startup, immediately before parsing
// plugin config and calling SetConfig
func (d *GenicamDevice) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *GenicamDevice) SetConfig(c *base.Config) error {
	// decode the plugin config
	var config Config
	if err := base.MsgPackDecode(c.PluginConfig, &config); err != nil {
		return err
	}

	// for example, convert the poll period from an HCL string into a time.Duration
	period, err := time.ParseDuration(config.FingerprintPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse doFingerprint period %q: %v", config.FingerprintPeriod, err)
	}
	d.fingerprintPeriod = period
    d.enabled = config.Enabled
	d.logger.Info("configured plugin", "config", log.Fmt("% #v", pretty.Formatter(config)))
	return nil
}

// Fingerprint streams detected devices.
// Messages should be emitted to the returned channel when there are changes
// to the devices or their health.
func (d *GenicamDevice) Fingerprint(ctx context.Context) (<-chan *device.FingerprintResponse, error) {
	// Fingerprint returns a channel. The recommended way of organizing a plugin
	// is to pass that into a long-running goroutine and return the channel immediately.
	outCh := make(chan *device.FingerprintResponse)
	go d.doFingerprint(ctx, outCh)
	return outCh, nil
}

// Stats streams statistics for the detected devices.
// Messages should be emitted to the returned channel on the specified interval.
func (d *GenicamDevice) Stats(ctx context.Context, interval time.Duration) (<-chan *device.StatsResponse, error) {
	// Similar to Fingerprint, Stats returns a channel. The recommended way of
	// organizing a plugin is to pass that into a long-running goroutine and
	// return the channel immediately.
	outCh := make(chan *device.StatsResponse)
	go d.doStats(ctx, outCh, interval)
	return outCh, nil
}

type reservationError struct {
	notExistingIDs []string
}

func (e *reservationError) Error() string {
	return fmt.Sprintf("unknown device IDs: %s", strings.Join(e.notExistingIDs, ","))
}

// Reserve returns information to the task driver on on how to mount the given devices.
// It may also perform any device-specific orchestration necessary to prepare the device
// for use. This is called in a pre-start hook on the client, before starting the workload.
func (d *GenicamDevice) Reserve(deviceIDs []string) (*device.ContainerReservation, error) {
	if len(deviceIDs) == 0 {
		return &device.ContainerReservation{}, nil
	}

    if !d.enabled {
        return nil, errPluginDisabled
    }

	d.logger.Info("reserving device ids", "deviceIDs", pretty.Formatter(deviceIDs))

    // This pattern can be useful for some drivers to avoid a race condition where a device disappears
    // after being scheduled by the server but before the server gets an update on the fingerprint
    // channel that the device is no longer available.
    d.deviceLock.RLock()
    var notExistingIDs []string
    for _, id := range deviceIDs {
        if _, deviceIDExists := d.devices[id]; !deviceIDExists {
            notExistingIDs = append(notExistingIDs, id)
        }
    }
    d.deviceLock.RUnlock()
    if len(notExistingIDs) != 0 {
        return nil, &reservationError{notExistingIDs}
    }

	// initialize the response
	resp := &device.ContainerReservation{
		Envs:    map[string]string{},
		Mounts:  []*device.Mount{},
		Devices: []*device.DeviceSpec{},
	}

	for index, serial_nbr := range deviceIDs {
		// Check if the device is known
        address, found := d.devices[serial_nbr]
		if !found {
			return nil, status.Newf(codes.InvalidArgument, "unknown device %q", serial_nbr).Err()
		}

        d.logger.Info("got device", "index", index, "address", address, "serial_nbr", serial_nbr)
		// Envs are a set of environment variables to set for the task.
		resp.Envs[deviceSerialNbr] = serial_nbr
		resp.Envs[deviceAddress] = address
	}

	return resp, nil
}
