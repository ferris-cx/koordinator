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
	"database/sql"
	"encoding/json"
	"fmt"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	nodeDevicesFilePath = "/tmp/koordinator/devieTopoInfo/"
)

// Device 表示一个设备
type Device struct {
	NodeID    int32                         `json:"nodeID"`
	PCIEID    string                        `json:"pcieID"`
	BusID     string                        `json:"busID,omitempty"`
	Health    bool                          `json:"health"`
	Xid       uint64                        `json:"xid"`
	Type      schedulingv1alpha1.DeviceType `json:"type,omitempty"`
	Labels    map[string]string             `json:"labels,omitempty"`
	UUID      string                        `json:"id,omitempty"`
	Minor     *int32                        `json:"minor,omitempty"`
	ModuleID  *int32                        `json:"moduleID,omitempty"`
	Resources corev1.ResourceList           `json:"resources,omitempty"`
}

// DeviceTree 表示设备树的节点
type DeviceTree struct {
	Device
	Children []*DeviceTree
}

// node_topo_info 结构体
type node_topo_info struct {
	Name     string          `json:"name"`
	IP       string          `json:"ip"`
	Topology json.RawMessage `json:"topology"`
}

var (
	db     *sql.DB
	isInit bool
)

func NewDeviceTree(nodeID int32, pcie string, busID string, deviceType schedulingv1alpha1.DeviceType) *DeviceTree {
	return &DeviceTree{
		Device: Device{
			NodeID: nodeID,
			PCIEID: pcie,
			BusID:  busID,
			Type:   deviceType,
			Health: true,
			Xid:    -1,
		},
		Children: []*DeviceTree{},
	}
}

func BuildDeviceTree(devices []*Device) map[int32]*DeviceTree {
	numaMap := make(map[int32]*DeviceTree)
	for _, device := range devices {
		klog.V(4).Infof("BuildDeviceTree : device%v", device)
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
			Device: Device{
				NodeID:    device.NodeID,
				PCIEID:    device.PCIEID,
				BusID:     device.BusID,
				Type:      device.Type,
				Health:    device.Health,
				Xid:       device.Xid,
				Labels:    device.Labels,
				UUID:      device.UUID,
				Minor:     device.Minor,
				ModuleID:  device.ModuleID,
				Resources: device.Resources,
			},
			Children: []*DeviceTree{},
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

func GenerateDeviceFiles(nodeDeviceInfos map[string]*nodeDevice) bool {
	if nodeDeviceInfos == nil || len(nodeDeviceInfos) <= 0 {
		fmt.Printf("nodeDeviceInfos is nil")
		return false
	}

	devsNodeMap := make(map[string][]*Device)
	for nodeName, nodeDeviceInfo := range nodeDeviceInfos {
		devices := make([]*Device, 0)
		for _, deviceTypeInfos := range nodeDeviceInfo.deviceInfos {
			for _, deviceInfo := range deviceTypeInfos {
				dv := &Device{
					Health:    deviceInfo.Health,
					Xid:       deviceInfo.Xid,
					NodeID:    deviceInfo.Topology.NodeID,
					PCIEID:    deviceInfo.Topology.PCIEID,
					BusID:     deviceInfo.Topology.BusID,
					Type:      deviceInfo.Type,
					UUID:      deviceInfo.UUID,
					Labels:    deviceInfo.Labels,
					Minor:     deviceInfo.Minor,
					ModuleID:  deviceInfo.ModuleID,
					Resources: deviceInfo.Resources,
				}
				devices = append(devices, dv)
			}
		}
		devsNodeMap[nodeName] = devices
	}

	nodeTopos := make([]*node_topo_info, len(devsNodeMap))
	for nodeName, devices := range devsNodeMap {
		klog.V(4).Infof("GenerateDeviceFiles:start for nodeName: %s", nodeName)
		time.Sleep(50)
		root := constructNodeDeviceTree(devices)

		jsonBytes, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			klog.V(4).Infof("Error marshaling to JSON: %v", err)
		}
		fmt.Println(string(jsonBytes))
		//writeToFile(string(jsonBytes) ,nodeDevicesFilePath + nodeName)
		topoInfo := &node_topo_info{
			Name:     nodeName,
			Topology: json.RawMessage(jsonBytes),
		}
		nodeTopos = append(nodeTopos, topoInfo)
		insertDB(topoInfo)
	}
	//batchInsertNodeTopoInfo()
	return true
}

func constructNodeDeviceTree(devices []*Device) *DeviceTree {
	root := NewDeviceTree(-1, "", "", "root")
	numaMap := BuildDeviceTree(devices)
	for _, numaTree := range numaMap {
		root.Children = append(root.Children, numaTree)
	}
	//PrintTree(root, 0)
	return root
}

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

func insertDB(info *node_topo_info) {
	if !isInit {
		if err := initDB(); err != nil {
			klog.Error("Failed to initialize database: %v", err)
		}
	} else {
		klog.V(5).Info("Database already initialized, skipping initialization.")
	}

	if err := insertNodeTopoInfo(info); err != nil {
		klog.V(4).Info("Failed to insert data: %v", err)
	} else {
		klog.V(4).Info("Data inserted successfully")
	}
}

func initDB() (err error) {
	dsn := "root:123456@tcp(192.168.10.203:30306)/k8s-database?charset=utf8mb4&parseTime=True&loc=Local"
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		return err
	}
	isInit = true
	return nil
}

func insertNodeTopoInfo(info *node_topo_info) error {
	klog.V(4).Infof("insertNodeTopoInfo: node_topo_info:%v", info)
	sql := `INSERT INTO node_device_topo (node, topology) VALUES (?, ?) ON DUPLICATE KEY UPDATE ip=VALUES(ip), topology=VALUES(topology)`
	_, err := db.Exec(sql, info.Name, info.Topology)
	return err
}

func batchInsertNodeTopoInfo(infos []node_topo_info) error {
	const batchSize = 1000

	sql := `INSERT INTO node_device_topo (node, topology) VALUES `
	values := make([]string, 0, len(infos))
	args := make([]interface{}, 0, 3*len(infos))

	for i, info := range infos {
		values = append(values, "(?, ?)")
		args = append(args, info.Name, info.Topology)

		if (i+1)%batchSize == 0 || i == len(infos)-1 {
			stmt := fmt.Sprintf("%s %s", sql, strings.Join(values, ", "))
			_, err := db.Exec(stmt, args...)
			if err != nil {
				return fmt.Errorf("failed to insert batch: %v", err)
			}

			values = values[:0]
			args = args[:0]
		}
	}
	return nil
}
