# 内存与发布配置

- 只有启用 TLS、未启用 REALITY、节点处于可用状态、使用 dns/http/tls
  内置 ACME，且编译包含 lego 时，才创建证书续期定时器。none/file、
  未启用 TLS、停用节点、minimal/nolego 均不创建任务。面板更新同步调整
  定时器；关闭后的过期回调不会重新创建任务。
- 限速器先 Load 在线用户 Map 和限速桶，缓存未命中时才创建并
  LoadOrStore；并发竞争仍使用最终胜出的对象。限速和设备数校验保留。
- 启动及本地配置热重载完成后调用 debug.FreeOSMemory；面板节点重建/
  重新启用成功后也调用一次，且在释放控制器锁后执行。普通轮询、
  未修改节点及仅更新用户时不强制释放。

## ConnectionConfig.BufferSize

这是 XrayR 本地 `config.yml` 的**顶层 ConnectionConfig** 配置，不放在
`Nodes[].ControllerConfig` 中，也不是面板 `custom_config` 的参数。
项目默认值和随包示例均为 `64`；内核将数值乘以 1024，所以实际单位是 KiB。
例如可用下面的配置进行较小缓冲的对比测试（不是已验证适合所有负载的推荐值）：

```yaml
ConnectionConfig:
  Handshake: 4
  ConnIdle: 30
  UplinkOnly: 2
  DownlinkOnly: 4
  BufferSize: 16 # KiB；项目默认 64，可对比 16/32/64 的吞吐与内存
```

在原有 ConnectionConfig 中修改 BufferSize 即可，不要重复创建 YAML 键。
修改本地配置会走现有配置热重载流程、重建服务，可能中断已有连接；
也可以在维护窗口重启。本文仅补充说明，没有修改默认值或运行配置。

生效范围与注意事项：

- XrayR 将其传入 Xray-core 的 level 0 policy.bufferSize。采用该策略并通过
  上下文传递缓冲策略的 pipe 路径才受影响；不是单个用户、整条连接或整个进程
  的严格内存上限。普通 dispatcher pipe 路径有上下行两条队列，不能简单用
  “连接数 × BufferSize”计算 RSS，也不是启动时为每条连接固定预分配这些内存。
- 缩小缓冲可能减少积压数据占用，但也可能增加背压、影响高吞吐或高延迟链路；
  应在相同并发/流量负载下比较 RSS、吞吐、延迟和 GC CPU，而非仅看空载内存。
- 当前固定版本的 Hysteria2 入站使用 DispatchLink 直接交付 Reader/Writer，
  不经过 XrayR getLink 创建上下行 pipe。因此不能声称调小此值就能限制
  Hysteria2 的 QUIC/UDP 缓冲；它也不控制 QUIC 连接/流接收窗口、socket 缓冲、
  geodata 或 lego 初始化内存。协议自身缓冲参数需另行核对。
- 内核中负值表示不设该缓冲大小限制，不适合用来降低内存；`0` 也不等于
  “零分配/完全不占内存”，仍有传输中的数据块与协议开销。

核对来源：`panel/defaultConfig.go`、`panel/panel.go:parseConnectionConfig`、
`release/config/config.yml.example`、`app/mydispatcher/default.go:getLink`，以及
当前 go.mod 固定的 core 中 `infra/conf/policy.go`、`transport/pipe` 和
`proxy/hysteria/server.go`。这与下方 GOMEMLIMIT 的运行时软目标是不同层级的控制。

## GOMEMLIMIT

镜像默认 ENV GOMEMLIMIT=40MiB，可通过 docker run -e GOMEMLIMIT=256MiB 覆盖。
直接运行二进制时在启动环境设置：

```sh
GOMEMLIMIT=40MiB ./XrayR --config /etc/XrayR/config.yml
```

systemd 可在服务 drop-in 的 [Service] 下设置 Environment=GOMEMLIMIT=40MiB，
然后 daemon-reload 并重启服务。这里仅提供配置方法，不修改本机运行中的服务。

40MiB 是 Go 运行时软内存目标，不是 RSS/容器上限；完整 lego、geodata、
连接缓冲等存活对象可能超过它。过低目标会增加 GC CPU 和延迟，
应按实际负载提高。FreeOSMemory 会触发 GC 并尝试归还空闲内存，
不保证固定 RSS，也不保证每次都执行特定的 madvise 系统调用。
参考 [Go GC 指南](https://go.dev/doc/gc-guide)。

## 构建与镜像

```sh
go build -tags nolego -trimpath -ldflags '-s -w -buildid=' -o XrayR
docker build --build-arg EDITION=nolego -t xrayr:nolego .
```

nolego 与 minimal 等价：都不链接 lego，禁止自动申请/续期，使用已有证书
请配置 CertMode: file。默认 EDITION=full 保留完整功能。
主 Docker 发布流程同时构建 full/minimal/nolego，后两者镜像 tag 带对应后缀。
Release 仍保留 minimal 下载包，无需再发布内容相同的 nolego 二进制包。
仓库内 Release、Docker 和测试发布的 go build 均使用 -trimpath 和 -s -w。

按用户确认，正式 Release 完整版和完整版镜像在构建末尾使用
upx --best --lzma，并以 upx -t 校验；minimal/nolego 不加壳。
UPX 不支持的目标告警并保留原始文件，校验失败则阻止发布损坏文件。
额外 UPX Actions Artifact 只包含完整版，已压缩的文件不会重复加壳。
加壳减少磁盘大小，不保证减少运行内存或改善启动速度。

## 本地验证

- 完整版与 nolego：证书定时器生命周期、Hysteria2 基础连接、用户限速和
  停用回归通过 race 检查；minimal+nolego 组合标签测试通过。
- nolego：Linux amd64、Windows 386、Android arm64 交叉编译通过。
- Apple M4/Go 1.27.0 上，已存在用户/IP/限速桶的缓存命中基准为
  41.83 ns/op、0 B/op、0 allocs/op；这不代表首次连接或全局设备限制也零分配。
- 工作流 YAML 和内嵌 shell 语法检查通过。本机未执行 Docker 构建，也未
  验证 Linux 加壳后的代理流量；Release 工作流增加了 Linux amd64 的 --help
  启动检查，实际执行结果应以 CI 为准。没有声称 RSS 可稳定在 40MiB 以下。
