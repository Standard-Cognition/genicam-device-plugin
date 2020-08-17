package main

import (
	log "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins"

	"github.com/Standard-Cognition/genicam-device-plugin/device"
)

func main() {
	// Serve the plugin
	plugins.Serve(factory)
}

// factory returns a new instance of our genicam device plugin
func factory(log log.Logger) interface{} {
	return device.NewGenicamDevice(log)
}
