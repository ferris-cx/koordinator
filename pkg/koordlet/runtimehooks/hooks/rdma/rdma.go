/*
Copyright 2022 The Koordinator Authors.

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

package rdma

import (
	"fmt"
	"strings"
	"encoding/json"
	"k8s.io/klog/v2"

	ext "github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"github.com/koordinator-sh/koordinator/pkg/koordlet/runtimehooks/hooks"
	"github.com/koordinator-sh/koordinator/pkg/koordlet/runtimehooks/protocol"
	rmconfig "github.com/koordinator-sh/koordinator/pkg/runtimeproxy/config"
)

const PCIDEVICE_KOORDINATOR_SH_RDMA = "PCIDEVICE_KOORDINATOR_SH_RDMA"
const PCIDEVICE_KOORDINATOR_SH_RDMA_INFO = "PCIDEVICE_KOORDINATOR_SH_RDMA_INFO"

type rdmaPlugin struct{}

// DeviceInfoForRdma represents the nested structure of the JSON object.
type DeviceInfoForRdma struct {
	Generic struct {
		DeviceID string `json:"deviceID"`
	} `json:"generic"`
}

// DeviceMap is a map that holds the device information.
type DeviceMap map[string]DeviceInfoForRdma

func (p *rdmaPlugin) Register(op hooks.Options) {
	klog.V(4).Infof("register rdma hook %v", "vf ENV inject")
	hooks.Register(rmconfig.PreCreateContainer, "rdma env inject", "inject annotations into container", p.InjectContainerRDMAEnv)
}

var singleton *rdmaPlugin

func Object() *rdmaPlugin {
	if singleton == nil {
		singleton = &rdmaPlugin{}
	}
	return singleton
}

func (p *rdmaPlugin) InjectContainerRDMAEnv(proto protocol.HooksProtocol) error {
	klog.V(4).Infof("rdma InjectContainerRDMAEnv start")
	containerCtx := proto.(*protocol.ContainerContext)
	if containerCtx == nil {
		return fmt.Errorf("container protocol is nil for plugin rdma")
	}
	containerReq := containerCtx.Request
	alloc, err := ext.GetDeviceAllocations(containerReq.PodAnnotations)
	klog.V(4).Infof("rdma alloc: %v", alloc)
	if err != nil {
		return err
	}
	devices, ok := alloc[schedulingv1alpha1.RDMA]
	if !ok || len(devices) == 0 {
		klog.V(4).Infof("no rdma alloc info in pod anno, %s", containerReq.PodMeta.Name)
		return nil
	}
	klog.V(4).Infof("rdma devices: %v", devices)

	rdmaVFs := []string{}
	for _, d := range devices {
		if d.Extension != nil {
			for _, vf := range d.Extension.VirtualFunctions {
				if vf.BusID != "" {
					rdmaVFs = append(rdmaVFs, vf.BusID)
				}
			}
		}
	}
	klog.V(4).Infof("rdma rdmaVFs:%v", rdmaVFs)
	if containerCtx.Response.AddContainerEnvs == nil {
		containerCtx.Response.AddContainerEnvs = make(map[string]string)
	}
	containerCtx.Response.AddContainerEnvs[PCIDEVICE_KOORDINATOR_SH_RDMA] = strings.Join(rdmaVFs, ",")

	rdmaJson, err := ConvertToJSON(rdmaVFs)
	klog.V(4).Infof("rdma rdmaJson:%v", rdmaJson)
	if err != nil {
		fmt.Errorf("convert to json error %w", err)
		return err
	}
	containerCtx.Response.AddContainerEnvs[PCIDEVICE_KOORDINATOR_SH_RDMA_INFO] = rdmaJson
	klog.V(4).Infof("rdma Response.Envs: %v", containerCtx.Response.AddContainerEnvs)
	return nil
}

// ConvertToJSON takes a slice of strings and converts it into a JSON string.
//{"0000:01:01.2":{"generic":{"deviceID":"0000:01:01.2"}},
// "0000:01:01.5":{"generic":{"deviceID":"0000:01:01.5"}}}
func ConvertToJSON(rdmaIDs []string) (string, error) {
	deviceMap := make(DeviceMap)

	for _, id := range rdmaIDs {
		deviceMap[id] = DeviceInfoForRdma{
			Generic: struct {
				DeviceID string `json:"deviceID"`
			}{
				DeviceID: id,
			},
		}
	}
	jsonBytes, err := json.Marshal(deviceMap)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}