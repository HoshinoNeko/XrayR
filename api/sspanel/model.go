package sspanel

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/xtls/xray-core/infra/conf"
)

// NodeInfoResponse is the response of node
type NodeInfoResponse struct {
	Group           int             `json:"node_group"`
	Class           int             `json:"node_class"`
	SpeedLimit      float64         `json:"node_speedlimit"`
	TrafficRate     float64         `json:"traffic_rate"`
	Sort            int             `json:"sort"`
	RawServerString string          `json:"server"`
	Type            string          `json:"type"`
	CustomConfig    json.RawMessage `json:"custom_config"`
	Version         string          `json:"version"`
	Enabled         *bool           `json:"enabled,omitempty"`
}

type CustomConfig struct {
	OffsetPortNode PortString       `json:"offset_port_node"`
	Host           string           `json:"host"`
	Method         string           `json:"method"`
	TLS            string           `json:"tls"`
	EnableVless    string           `json:"enable_vless"`
	Network        string           `json:"network"`
	Security       string           `json:"security"`
	Path           string           `json:"path"`
	VerifyCert     bool             `json:"verify_cert"`
	Obfs           string           `json:"obfs"`
	Header         json.RawMessage  `json:"header"`
	AllowInsecure  any              `json:"allow_insecure"`
	Servicename    string           `json:"servicename"`
	EnableXtls     string           `json:"enable_xtls"`
	Flow           string           `json:"flow"`
	EnableREALITY  bool             `json:"enable_reality"`
	RealityOpts    *REALITYConfig   `json:"reality-opts"`
	Hysteria2      *Hysteria2Config `json:"hysteria2"`
}

// PortString accepts both JSON integer and string ports used by panel editors.
type PortString string

func (p *PortString) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		value = string(raw)
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be an integer between 1 and 65535")
	}
	*p = PortString(strconv.Itoa(port))
	return nil
}

type Hysteria2Config struct {
	Version        int32              `json:"version"`
	UDPIdleTimeout int64              `json:"udpIdleTimeout"`
	Masquerade     conf.Masquerade    `json:"masquerade"`
	FinalMask      *conf.FinalMask    `json:"finalmask"`
	PortHopping    *PortHoppingConfig `json:"portHopping"`
}

type PortHoppingConfig struct {
	Enabled               bool   `json:"enabled"`
	AutoConfigureFirewall bool   `json:"autoConfigureFirewall"`
	Ports                 string `json:"ports"`
}

// UserResponse is the response of user
type UserResponse struct {
	ID          int     `json:"id"`
	Passwd      string  `json:"passwd"`
	Port        uint32  `json:"port"`
	Method      string  `json:"method"`
	SpeedLimit  float64 `json:"node_speedlimit"`
	DeviceLimit int     `json:"node_iplimit"`
	UUID        string  `json:"uuid"`
	AliveIP     int     `json:"alive_ip"`
}

// Response is the common response
type Response struct {
	Ret  uint            `json:"ret"`
	Data json.RawMessage `json:"data"`
}

// PostData is the data structure of post data
type PostData struct {
	Data     interface{} `json:"data"`
	ReportID string      `json:"report_id,omitempty"`
}

// SystemLoad is the data structure of system load
type SystemLoad struct {
	Uptime string `json:"uptime"`
	Load   string `json:"load"`
}

// OnlineUser is the data structure of online user
type OnlineUser struct {
	UID int    `json:"user_id"`
	IP  string `json:"ip"`
}

// UserTraffic is the data structure of traffic
type UserTraffic struct {
	UID      int   `json:"user_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

type RuleItem struct {
	ID      int    `json:"id"`
	Content string `json:"regex"`
}

type IllegalItem struct {
	ID  int `json:"list_id"`
	UID int `json:"user_id"`
}

type REALITYConfig struct {
	Dest             string   `json:"dest,omitempty"`
	ProxyProtocolVer uint64   `json:"proxy_protocol_ver,omitempty"`
	ServerNames      []string `json:"server_names,omitempty"`
	PrivateKey       string   `json:"private_key,omitempty"`
	MinClientVer     string   `json:"min_client_ver,omitempty"`
	MaxClientVer     string   `json:"max_client_ver,omitempty"`
	MaxTimeDiff      uint64   `json:"max_time_diff,omitempty"`
	ShortIds         []string `json:"short_ids,omitempty"`
}
