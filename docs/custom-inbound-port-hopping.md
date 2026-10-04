# 自定义 Hysteria2 入站的自动端口转发

适用于本分支的 XrayR，包含完整版、minimal 和 nolego。
此配置由 XrayR 解析，不是 xray-core 原生的 `quicParams` 字段。

## 推荐写法

在 `custom_inbound.json` 的 Hysteria2 入站对象顶层增加：

```json
"portHopping": {
  "enabled": true,
  "autoConfigureFirewall": true,
  "ports": "55001-60000"
}
```

例如完整的配置骨架如下，认证密码、证书路径均为占位符：

```json
[
  {
    "tag": "static-hy2",
    "listen": "0.0.0.0",
    "port": 55444,
    "protocol": "hysteria",
    "portHopping": {
      "enabled": true,
      "autoConfigureFirewall": true,
      "ports": "55001-60000"
    },
    "settings": {
      "version": 2,
      "users": [
        {"auth": "example:REPLACE_WITH_PASSWORD", "email": "example", "level": 0}
      ]
    },
    "streamSettings": {
      "network": "hysteria",
      "security": "tls",
      "tlsSettings": {
        "certificates": [
          {
            "certificateFile": "/path/to/fullchain.pem",
            "keyFile": "/path/to/private-key.pem"
          }
        ]
      },
      "hysteriaSettings": {"version": 2, "udpIdleTimeout": 60},
      "finalmask": {
        "quicParams": {
          "congestion": "force-brutal",
          "brutalUp": "60 mbps",
          "brutalDown": "0"
        }
      }
    }
  }
]
```

`config.yml` 指定：

```yaml
InboundConfigPath: /etc/XrayR/custom_inbound.json
```

保留原有路由、出站以及面板节点配置，不要用此片段覆盖整个 `config.yml`。

两项开关都为 JSON 布尔值 `true` 时才自动安装转发。`ports` 支持逗号分隔
的端口或递增范围，例如 `55001-60000,61000`。目标端口取自入站 `port`，不需要
重复填写；自动配置要求非空且唯一的 `tag`、单个有效监听端口和 IP 监听地址。

## 兼容现有写法

也可保留此前的 `streamSettings.finalmask.quicParams.udpHop`：

```json
"udpHop": {
  "enable": true,
  "autoConfigureFirewall": true,
  "ports": "55001-60000",
  "interval": "5-10"
}
```

XrayR 会将其提取成同一种防火墙计划。`enable` 与 `enabled` 均接受布尔值；
如果同时填写，值必须相同。顶层 `portHopping` 优先于嵌套 `udpHop`，配置对象
整体覆盖，不合并开关和端口。顶层显式关闭或填写空对象时，不会回退到嵌套开启配置。

面板节点也支持相同语义，优先级为：`custom_config.portHopping` >
`custom_config.hysteria2.portHopping` >
`custom_config.hysteria2.finalmask.quicParams.udpHop`。
SSPanel 的客户端订阅使用同一顺序，以免服务端规则与客户端跳端口范围不一致。

`interval` 只描述客户端跳端口周期，不控制服务端 NAT，服务端不会使用它。
客户端仍需自行设置 `55001-60000` 的端口范围和需要的周期，本地静态用户不会
自动获得 SSPanel 订阅。原生客户端 `finalmask.udp` 中的 `udphop` 不应用到服务端；
XrayR 会剔除该客户端专用 mask，保留 Salamander 等其他 mask。

`brutalUp`、`brutalDown` 是 xray-core 的字符串字段，不能写成数字 `0`。
其他 QUIC 字段仍由 xray-core 验证。

## 防火墙与生命周期

- Linux 下使用与面板节点相同的规则管理器，不依赖 UFW。
- `listen` 为 `0.0.0.0`、`::` 或省略时，配置 IPv4/IPv6 两套规则。
  具体 IPv4/IPv6 地址只配置对应协议族。
- 检测命令、NAT 表能力以及 root/CAP_NET_ADMIN 权限；双栈环境需要
  `iptables` 和 `ip6tables`。所需任一协议族失败，启动失败且清理本次规则。
- 使用独立的 `custom-inbound:` 所有权标识，不复用面板节点规则标识；
  重复启动不重复加规则，异常退出遗留的相同规则在下次启动时检查并接管。
- 正常关闭、任一后续面板服务启动失败以及部分防火墙安装失败时，清理静态规则。
  SIGKILL/断电无法执行清理；不要把进程退出当作必然删除内核规则。
- 本地配置文件没有新增文件监控热重载；修改后需重启 XrayR，重启会重读文件。
- 这里只增加 UDP NAT 重定向，不自动开放 INPUT、防火墙安全组或容器映射。
- 静态入站的认证、用户同步和计费语义不变，不因自动转发而参与面板计费。

升级到包含此功能的二进制，重启后检查：

```sh
iptables -t nat -S PREROUTING
ip6tables -t nat -S PREROUTING
ss -lunp
```

上述示例应将两种协议族的 UDP `55001:60000` 重定向到 `55444`。
实际跳端口连接仍需在目标服务器上用客户端分别通过 IPv4/IPv6 验证。
