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

package deviceshare

import (
	"encoding/json"
	"fmt"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"k8s.io/klog/v2"
	"os"
	"time"
)

const (
	nodeDevicesFilePath      = "/tmp/koordinator/devieTopoInfo/"
)

// Device 表示一个设备
type Device struct {
	NodeID int32 `json:"nodeID"`
	PCIEID string `json:"pcieID"`
	BusID string `json:"busID,omitempty"`
	Type schedulingv1alpha1.DeviceType `json:"type,omitempty"`
	UUID string `json:"id,omitempty"`
}

// DeviceTree 表示设备树的节点
type DeviceTree struct {
	NodeID  int32
	PCIEID    string
	BusID   string
	Type schedulingv1alpha1.DeviceType `json:"type,omitempty"`
	Children []*DeviceTree
}

// NewDeviceTree 创建一个新的设备树节点
func NewDeviceTree(nodeID int32, pcie string, busID string, deviceType schedulingv1alpha1.DeviceType) *DeviceTree {
	return &DeviceTree{
		NodeID:  nodeID,
		PCIEID:  pcie,
		BusID:   busID,
		Type:    deviceType,
		Children: []*DeviceTree{},
	}
}

// BuildDeviceTree 根据设备数组构建设备树
func BuildDeviceTree(devices []*Device) map[int32]*DeviceTree {
	numaMap := make(map[int32]*DeviceTree)
	for _, device := range devices {
		if _, exists := numaMap[device.NodeID]; !exists {
			numaMap[device.NodeID] = NewDeviceTree(device.NodeID, "", "", "NUMA")
		}

		root := numaMap[device.NodeID]
		pcieMap := make(map[string]*DeviceTree)
		for _, tree := range root.Children {
			if tree.PCIEID == device.PCIEID {
				pcieMap[device.PCIEID] = tree
				break
			}
		}

		if _, exists := pcieMap[device.PCIEID]; !exists {
			pcieMap[device.PCIEID] = NewDeviceTree(device.NodeID, device.PCIEID, "", "PCIE")
			root.Children = append(root.Children, pcieMap[device.PCIEID])
		}

		pcieMap[device.PCIEID].Children = append(pcieMap[device.PCIEID].Children, &DeviceTree{
			NodeID:  device.NodeID,
			PCIEID:  device.PCIEID,
			BusID:   device.BusID,
			Type:    device.Type,
		})
	}
	return numaMap
}

// PrintTree 打印设备树
func PrintTree(tree *DeviceTree, level int) {
	if tree == nil {
		return
	}

/*	fmt.Printf("%sNodeID: %d, Pcie: %s, BusID:%s\n", strings.Repeat("  ", level), tree.NodeID, tree.Pcie, tree.BusID)*/
	for _, child := range tree.Children {
		klog.V(4).Infof("PrintTree : child%v", child)
		PrintTree(child, level+1)
	}
}

//生成节点设备文件 func GetMinNum(pod *corev1.Pod) (int, error) {
func GenerateDeviceFiles(nodeDeviceInfos map[string]*nodeDevice) bool{
	if nodeDeviceInfos == nil || len(nodeDeviceInfos)<=0 {
		fmt.Printf("nodeDeviceInfos is nil")
		return false
	}

	devsNodeMap  := make(map[string][]*Device)
	for nodeName, nodeDeviceInfo := range nodeDeviceInfos {
		devices := make([]*Device, 0)
		for _, deviceTypeInfos := range nodeDeviceInfo.deviceInfos {//按照设备类型遍历
			for _, deviceInfo := range deviceTypeInfos {//遍历设备列表
				dv := &Device{
					NodeID: deviceInfo.Topology.NodeID,
					PCIEID: deviceInfo.Topology.PCIEID,
					BusID: deviceInfo.Topology.BusID,
					Type: deviceInfo.Type,
					UUID: deviceInfo.UUID,
				}
				devices = append(devices, dv)
			}
		}
		devsNodeMap[nodeName] = devices
	}

	for nodeName, devices := range devsNodeMap{
		klog.V(4).Infof("GenerateDeviceFiles:start for nodeName: %s", nodeName)
		time.Sleep(50)
		root := constructNodeDeviceTree(devices)

		jsonBytes, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			klog.V(4).Infof("Error marshaling to JSON: %v", err)
		}
		fmt.Println(string(jsonBytes))
		writeToFile(string(jsonBytes) ,nodeDevicesFilePath + nodeName)
	}
	return true
}

//生成节点设备树
func constructNodeDeviceTree(devices []*Device) *DeviceTree{
	root := NewDeviceTree(-1, "", "", "root")
	numaMap := BuildDeviceTree(devices)
	for _, numaTree := range numaMap {
		root.Children = append(root.Children, numaTree)
	}
	//PrintTree(root, 0)
	return root
}

//writeToFile 将字符串内容写入指定的文件
func writeToFile(content string, filename string) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	return nil
}