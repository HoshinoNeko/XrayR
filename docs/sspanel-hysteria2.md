# SSPanel-UIM Hysteria2 (`sort = 15`)

XrayR uses Xray-core's native `hysteria` protocol with `version: 2`. Each
SSPanel user UUID is used as that user's Hysteria2 authentication password.

The XrayR node must use `PanelType: SSpanel`. `NodeType` may be left at its
existing value because an SSPanel node with `sort: 15` is negotiated as
`Hysteria2`. TLS is mandatory; configure XrayR `CertConfig` with `file`, `dns`,
`http`, or `tls` mode.

Example SSPanel `custom_config`:

```json
{
  "offset_port_node": "443",
  "offset_port_user": "443",
  "host": "hy.example.com",
  "allow_insecure": false,
  "hysteria2": {
    "version": 2,
    "udpIdleTimeout": 60,
    "masquerade": {
      "type": "404"
    },
    "finalmask": {
      "udp": [
        {
          "type": "salamander",
          "settings": {
            "password": "change-this-obfs-password"
          }
        }
      ],
      "quicParams": {
        "congestion": "bbr",
        "brutalUp": "100 mbps",
        "brutalDown": "100 mbps",
        "initStreamReceiveWindow": 8388608,
        "maxStreamReceiveWindow": 8388608,
        "initConnectionReceiveWindow": 20971520,
        "maxConnectionReceiveWindow": 20971520,
        "maxIdleTimeout": 30,
        "keepAlivePeriod": 10,
        "disablePathMTUDiscovery": false,
        "maxIncomingStreams": 1024
      }
    },
    "portHopping": {
      "enabled": true,
      "autoConfigureFirewall": true,
      "ports": "20000-30000"
    }
  }
}
```

`masquerade` and `finalmask` are passed without a reduced intermediate schema,
so every field supported by Xray-core v26.3.27 remains available. The separate
`portHopping` object controls deployment behavior and subscription output.

When both port-hopping switches are enabled, XrayR first verifies that it is on
Linux, that `iptables` (or `ip6tables` for an IPv6 listen address) is installed, and that the process has permission to
read the NAT table. It then adds idempotent UDP `REDIRECT` rules and removes
them on node disable, configuration replacement, rollback, or shutdown. XrayR
fails the node activation if the preflight or rule installation fails. With
`autoConfigureFirewall: false`, provision equivalent UDP forwarding outside
XrayR.

Subscriptions are generated for Mihomo/Clash, sing-box, Xray JSON, the generic
V2Ray URI list, and the dedicated `/sub/{token}/hysteria2` endpoint. Protocol-
specific SS, SIP002, SIP008, and Trojan formats cannot encode a Hysteria2 node
and continue to return their own supported node types.
