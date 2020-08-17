package aravis

// #cgo pkg-config: aravis-0.8
// #include <arv.h>
// #include <stdlib.h>
import "C"

type ArvDevice struct {
    id C.uint
}

func (d *ArvDevice) Id() (string, error) {
	s, err := C.arv_get_device_id(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) PhysicalId() (string, error) {
	s, err := C.arv_get_device_physical_id(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) Model() (string, error) {
	s, err := C.arv_get_device_model(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) SerialNbr() (string, error) {
	s, err := C.arv_get_device_serial_nbr(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) Vendor() (string, error) {
	s, err := C.arv_get_device_vendor(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) Address() (string, error) {
	s, err := C.arv_get_device_address(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) Protocol() (string, error) {
	s, err := C.arv_get_device_protocol(d.id)
	return C.GoString(s), err
}

func (d *ArvDevice) InterfaceId() (string, error) {
	s, err := C.arv_get_interface_id(d.id)
	return C.GoString(s), err
}

func GetDevices() ([]*ArvDevice, error) {
    ndevices, err := GetNumDevices()

    if err != nil {
        return nil, err
    }

    devices := make([]*ArvDevice, 0, ndevices)

    for id := uint(0); id < ndevices; id++ {
        devices = append(devices, &ArvDevice {id: C.uint(id)})
    }

    return devices, err
}

func GetNumDevices() (uint, error) {
	n, err := C.arv_get_n_devices()
	return uint(n), err
}

func UpdateDeviceList() {
	C.arv_update_device_list()
}
