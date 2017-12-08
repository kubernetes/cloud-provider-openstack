package cinder

import (
	"golang.org/x/net/context"
)

var fakeNodeID = "CSINodeID"
var fakeEndpoint = "tcp://127.0.0.1:10000"
var fakeCtx = context.Background()
var fakeVolName = "CSIVolumeName"
var fakeVolID = "CSIVolumeID"
var fakeVolType = "lvmdriver-1"
var fakeAvailability = "nova"
var fakeDevicePath = "/dev/xxx"
var fakeTargetPath = "/mnt/cinder"
