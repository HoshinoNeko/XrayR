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
