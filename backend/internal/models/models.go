package models

import "encoding/json"

type Provider struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	APIBaseURL     string `json:"apiBaseUrl"`
	TokenType      string `json:"tokenType"`
	Enabled        bool   `json:"enabled"`
	TokenMasked    string `json:"tokenMasked"`
	LastTestStatus string `json:"lastTestStatus"`
	LastTestError  string `json:"lastTestError"`
	LastTestAt     string `json:"lastTestAt"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}
type Machine struct {
	ID                string `json:"id"`
	ProviderID        int64  `json:"providerId"`
	Name              string `json:"name"`
	Remark            string `json:"remark"`
	Region            string `json:"region"`
	Status            string `json:"status"`
	PublicIPv4        string `json:"publicIPv4"`
	PublicIPv6        string `json:"publicIPv6"`
	HealthStatus      string `json:"healthStatus"`
	TrafficText       string `json:"trafficText"`
	ExpireTime        string `json:"expireTime"`
	LastCheckAt       string `json:"lastCheckAt"`
	LastRotationAt    string `json:"lastRotationAt"`
	CooldownUntil     string `json:"cooldownUntil"`
	ExpectedPort      int    `json:"expectedPort"`
	FailureCount      int    `json:"failureCount"`
	SuccessCount      int    `json:"successCount"`
	AutoRotateEnabled bool   `json:"autoRotateEnabled"`
	RawProviderJSON   string `json:"-"`
}
type Agent struct {
	ID              string `json:"id"`
	MachineID       string `json:"machineId"`
	TokenPrefix     string `json:"tokenPrefix"`
	Name            string `json:"name"`
	Hostname        string `json:"hostname"`
	AgentVersion    string `json:"agentVersion"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	PublicIPv4      string `json:"publicIPv4"`
	PublicIPv6      string `json:"publicIPv6"`
	Status          string `json:"status"`
	LastHeartbeatAt string `json:"lastHeartbeatAt"`
	CreatedAt       string `json:"createdAt"`
	Revoked         bool   `json:"revoked"`
}
type Probe struct {
	ID              int64  `json:"id"`
	MachineID       string `json:"machineId"`
	Name            string `json:"name"`
	Source          string `json:"source"`
	Type            string `json:"type"`
	TargetHost      string `json:"targetHost"`
	URL             string `json:"url"`
	ExpectedStatus  string `json:"expectedStatus"`
	TargetPort      int    `json:"targetPort"`
	TimeoutMS       int    `json:"timeoutMs"`
	IntervalSeconds int    `json:"intervalSeconds"`
	FailureWeight   int    `json:"failureWeight"`
	Enabled         bool   `json:"enabled"`
}
type ProviderMachine struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Remark      string          `json:"remark"`
	Region      string          `json:"region"`
	Status      string          `json:"status"`
	PublicIPv4  string          `json:"publicIPv4"`
	PublicIPv6  string          `json:"publicIPv6"`
	TrafficText string          `json:"trafficText"`
	ExpireTime  string          `json:"expireTime"`
	Raw         json.RawMessage `json:"-"`
}
type DNSProvider struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ProviderType    string `json:"providerType"`
	TokenMasked     string `json:"tokenMasked"`
	ExtraConfigJSON string `json:"extraConfigJson"`
	Enabled         bool   `json:"enabled"`
}
type DNSRecord struct {
	ID                int64  `json:"id"`
	MachineID         string `json:"machineId"`
	DNSProviderID     int64  `json:"dnsProviderId"`
	RecordName        string `json:"recordName"`
	RecordType        string `json:"recordType"`
	ZoneID            string `json:"zoneId"`
	Proxied           bool   `json:"proxied"`
	TTL               int    `json:"ttl"`
	Enabled           bool   `json:"enabled"`
	SyncAfterRotation bool   `json:"syncAfterRotation"`
	LastIP            string `json:"lastIp"`
	LastSyncStatus    string `json:"lastSyncStatus"`
	LastSyncError     string `json:"lastSyncError"`
	LastSyncAt        string `json:"lastSyncAt"`
}
