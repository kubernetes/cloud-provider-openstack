/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	"k8s.io/cloud-provider-openstack/pkg/flexvolume"
	"k8s.io/cloud-provider-openstack/pkg/flexvolume/uuid"
	"k8s.io/klog"
)

func main() {
	// Glog requires this otherwise it complains.
	flag.CommandLine.Parse(nil)
	// This is a temporary hack to enable proper logging until upstream dependencies
	// are migrated to fully utilize klog instead of glog.
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	driver := flexvolume.NewFlexVolumeDriver(uuid.NewUUID())
	os.Stdout.WriteString(driver.Run(os.Args[1:]))
}
