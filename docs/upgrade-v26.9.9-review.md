# v26.9.9 升级验证与待 review 项

## 当前状态

本地已适配官方 `v26.9.9`（`52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120`），
保留此前审核通过的 TLS 快照补丁。升级尚未推送、尚未部署。
`go.mod` 临时替换到 `/Users/hoshino/Documents/Codex/xray-core-tls-v26.9.9`，
验收通过后必须替换为 fork 的固定远程提交，不能直接发布当前本地路径依赖。

已通过：全项目编译；无混淆/Salamander TCP 和小 UDP、停用恢复、重载回滚、
计费生命周期竞态测试；面板接口计费契约；PHP 配置/订阅契约；三轮 TLS 证书竞态测试。
未通过：4 KB UDP 回程完整性、纯远端 udphop 的真实握手。16 KB 单数据报尚未验收；
`core.Dial` 写入会按核心缓冲区拆分，不能据此声称支持任意大小的单个 UDP 数据报。

## 参数变化

- 协议仍为 `hysteria`，`version: 2`；UUID 认证方式不变。
- `udpIdleTimeout` 仍为 0（默认 60）或 2–600 秒，`masquerade` 仍支持。
- `finalmask.udp` 的 `salamander` 及 `settings.password` 仍支持；密码至少 4 字节。
  当前示例无需删除。新增 `packetSize` 会选择 Gecko 变体，不能视为其他 HY2 客户端
  普通 Salamander 的等价参数，跨客户端配置应保留只含 password 的形式。
- `finalmask.quicParams.udpHop` 移至 `finalmask.udp` 的 `type: udphop`，参数为
  `mode`、`interval`、`remotePorts`、`remoteIPs`、`sockopt`。interval 运行时要求至少 5 秒。
  核心支持的模式包含 `intervallocal`、`intervalremote`、`perconnremote` 及逗号组合。
  udphop 仅客户端支持，必须是 UDP 掩码数组最后一项（核心反向套用）。
  XrayR 服务端剔除该客户端掩码，防火墙仍由独立 portHopping 开关管理。
  面板保留旧字段读取兼容并转换输出；显式关闭 portHopping 优先。
- QUIC 新增 `bbrProfile`（conservative/standard/aggressive，默认 standard）、
  `brutalDisableLossCompensation`、`disableChromeParrot`、`disableGSO`、
  `disableStatelessReset`，通过原生类型透传，不自动改变用户选择。
- Go 最低版本变为 1.27；已适配统计计数器、嗅探匹配器和配置类型变化。
- 非 HY2 影响：Shadowsocks none/plain 已由上游移除，不再伪装支持；freedom 默认
  限制私网目标。回环放行仅用于本地测试，不放宽生产策略。

## 新发现问题及建议（尚未修改核心实现）

### 1. HY2 UDP 回程截断

真实测试发送 4096 字节，echo 服务记录 `[11, 4096]`，客户端回程仅得 3528 字节。
定位到 `proxy/hysteria/client.go` 的 `UDPReader.ReadFrom` 使用固定
`hysteria.MaxDatagramFrameSize`（1200）的接收数组；`InterConn.Read` 使用 copy
返回，较大的帧可能被静默截断。客户端自 2026-09-01 起省略最大 Datagram 帧参数，
实际可收帧上限不能再简单等同于固定 1200。

建议：明确接收缓冲区上限与实际 QUIC 数据帧上限；消除静默截断，补充超过 1200
字节的帧、4 KB 数据报分片/重组和双向回传测试。另行验证核心缓冲区所支持的单
数据报上限，不承诺未经验证的 16 KB/64 KB 支持。

### 2. udphop 纯远端模式收包阻塞

`transport/internet/finalmask/udphop/conn.go` 的 `hop` 只在 local=true 时启动
recv 协程。`mode: intervalremote` 使用原始 socket，却没有协程向 readCh 投递数据，
真实握手因此阻塞。

建议：非本地跳跃模式初始化原始 socket 的唯一收包协程，配套 WaitGroup/关闭路径；
验证 intervalremote、perconnremote、本地+远端组合和关闭行为。不要把纯远端模式
偷偷改成改变本地源端口的模式，也不要删除端口跳跃功能来掩盖失败。

两项修复需要用户 review 后再实施。完成后重跑连接、分片、计费、停用、限速和
订阅加载回归，再将 v26.9.9 补丁版本推送到 fork 并锁定 XrayR 远程依赖。

## 核对源码

- https://github.com/XTLS/Xray-core/blob/v26.9.9/infra/conf/transport_finalmask.go
- https://github.com/XTLS/Xray-core/blob/v26.9.9/infra/conf/transport_method.go
- https://github.com/XTLS/Xray-core/blob/v26.9.9/proxy/hysteria/client.go
- https://github.com/XTLS/Xray-core/blob/v26.9.9/transport/internet/hysteria/conn.go
- https://github.com/XTLS/Xray-core/blob/v26.9.9/transport/internet/finalmask/udphop/conn.go
