/*
Copyright 2021.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorldSpec defines the desired state of World
type WorldSpec struct {
	ServerProperties *ServerProperties `json:"serverProperties"`
	Version          string            `json:"version"`
	Name             string            `json:"name"`
	ColdStart        bool              `json:"cold_start"`
}

// ServerProperties is the actual server properties
type ServerProperties struct {
	EnableJMXMonitoring            bool   `json:"enableJMXMonitoring,omitempty,omitempty"`
	RCONPort                       int    `json:"rconPort,omitempty"`
	LevelSeed                      string `json:"levelSeed,omitempty"`
	Gamemode                       string `json:"gamemode,omitempty"`
	EnableCommandBlock             bool   `json:"enableCommandBlock,omitempty"`
	EnableQuery                    bool   `json:"enableQuery,omitempty"`
	GeneratorSettings              string `json:"generatorSettings,omitempty"`
	LevelName                      string `json:"levelName,omitempty"`
	MOTD                           string `json:"motd,omitempty"`
	QueryPort                      int    `json:"queryPort,omitempty"`
	PVP                            bool   `json:"pvp,omitempty"`
	GenerateStructures             bool   `json:"generateStructure,omitempty"`
	Difficulty                     string `json:"difficulty,omitempty"`
	NetworkCompressionThreshold    int    `json:"networkCompressionThreshold,omitempty"`
	MaxTickTime                    int    `json:"maxTickTime,omitempty"`
	MaxPlayers                     int    `json:"maxPlayers,omitempty"`
	UseNativeTransport             bool   `json:"useNativeTransport,omitempty"`
	OnlineMode                     bool   `json:"onlineMode,omitempty"`
	EnableStatus                   bool   `json:"enableStatus,omitempty"`
	AllowFlight                    bool   `json:"allowFlight,omitempty"`
	BroadcastRCONToOps             bool   `json:"broadcaseRCONToOps,omitempty"`
	ViewDistance                   int    `json:"viewDistance,omitempty"`
	MaxBuildHeight                 int    `json:"maxBuildHeight,omitempty"`
	ServerIP                       string `json:"serverIP,omitempty"`
	AllowNether                    bool   `json:"allowNether,omitempty"`
	ServerPort                     int    `json:"serverPort,omitempty"`
	EnableRCON                     bool   `json:"enableRCON,omitempty"`
	SyncChunkWrites                bool   `json:"syncChunkWrites,omitempty"`
	OpPermissionLevel              int    `json:"opPermissionLevel,omitempty"`
	PreventProxyConnection         bool   `json:"preventProxyConnections,omitempty"`
	ResourcePack                   string `json:"resourcePack,omitempty"`
	EntityBroadcaseRangePercentage int    `json:"entityBroadcastPercentage,omitempty"`
	RCONPassword                   string `json:"rconPassword,omitempty"`
	PlayerIdleTimeout              int    `json:"playerIdleTimeout,omitempty"`
	ForceGamemode                  bool   `json:"forceGamemode,omitempty"`
	RateLimit                      int    `json:"rateLimit,omitempty"`
	Hardcore                       bool   `json:"hardcore,omitempty"`
	WhiteList                      bool   `json:"whiteList,omitempty"`
	BroadcastConsoleToOps          bool   `json:"broadcastConsoleToOps,omitempty"`
	SpawnNPCS                      bool   `json:"spawnNPCS,omitempty"`
	SpawnAnimals                   bool   `json:"spawnAnimals,omitempty"`
	SnooperEnabled                 bool   `json:"snooperEnabled,omitempty"`
	FunctionPermissionLevel        int    `json:"functionPermissionLevel,omitempty"`
	LevelType                      string `json:"levelType,omitempty"`
	SpawnMonster                   bool   `json:"spawnMonster,omitempty"`
	EnforceWhitelist               bool   `json:"enforceWhitelist,omitempty"`
	ResourcePackSha1               string `json:"resourcePackSha1,omitempty"`
	SpawnProtection                int    `json:"spawnProtection,omitempty"`
	MaxWorldSize                   int    `json:"maxWorldSize,omitempty"`
}

// WorldStatus defines the observed state of World
type WorldStatus struct {
	Ready bool `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// World is the Schema for the worlds API
type World struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorldSpec   `json:"spec,omitempty"`
	Status WorldStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorldList contains a list of World
type WorldList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []World `json:"items"`
}

func init() {
	SchemeBuilder.Register(&World{}, &WorldList{})
}
