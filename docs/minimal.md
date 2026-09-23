# Minimal 版本

Release 同时提供原名称的完整版和后缀为 `-minimal.zip` 的精简版。
包内可执行文件仍叫 `XrayR`（Windows 为 `XrayR.exe`），无需修改服务启动命令。
MIPS 包中的 softfloat 可执行文件也使用相同的 minimal 构建配置。

Minimal 完全排除 lego 自动证书签发和续期实现，包括 DNS-01、HTTP-01、
TLS-ALPN-01；并非仅减少 DNS 服务商。配置 `CertMode: dns/http/tls` 时会明确报错，
不会自动降级到明文，也不会因磁盘存在缓存证书而跳过检查。
完整版本的这些功能保持不变。

Minimal 保留原有协议、面板对接、计费、流量上报、限速和已有证书加载代码。
它不是对这些功能或所有平台运行时的额外兼容性保证。
Hysteria2 仍必须使用 TLS；不要用 `CertMode: none` 代替证书。

请通过外部工具申请、续期证书，设置：

```yaml
CertConfig:
  CertMode: file
  CertFile: /etc/XrayR/cert/server.crt
  KeyFile: /etc/XrayR/cert/server.key
```

将上述配置放在对应节点的 ControllerConfig 下。续期后应确认服务加载了新证书，
必要时重启服务。随包配置示例默认改为 file，但仍需替换示例路径并提供有效证书。
无需自动证书的其他配置（例如符合原协议要求的 none/REALITY）沿用现有行为。

本地编译：

```sh
CGO_ENABLED=0 go build -mod=readonly -tags minimal -trimpath -ldflags '-s -w -buildid=' -o XrayR
```

Android 还需要在 ldflags 中加入 `-checklinkname=0`，与完整版一致。
不加 minimal 标签即为完整版。go.mod/go.sum 仍保留完整版依赖，构建前的
`go mod download` 可能仍下载 lego；这不代表 minimal 链接了 lego。
CI 使用 `go list -tags minimal -deps .` 检查其不在最终依赖图中。

## UPX 评估

使用独立的副本测试，不改变未压缩的发布文件：

```sh
upx --best --lzma -o XrayR-upx XrayR
upx -t XrayR-upx
upx -d -o XrayR-restored XrayR-upx
cmp XrayR XrayR-restored
```

UPX 压缩的是可执行文件在磁盘上的表示，不保证减少运行内存。
解包完整性检查不等同于目标系统运行测试。不同操作系统、架构和安全策略
需要单独验证；不能把 Linux amd64 的结果推广到 Android、macOS 或所有平台。
正常 Release 文件不自动应用 UPX，避免改变完整/minimal 两版的运行兼容性。
发布 Release 或手动触发 Build and Release 工作流（workflow_dispatch，可选择分支）时，
额外生成 Actions-only 的 UPX 副本（完整/minimal 两版），
放在名为 `XrayR-<平台>-upx` 的 Actions Artifact 内，不上传到 GitHub Release。
其中 tar.gz 保留可执行权限，附 SHA-256 校验文件及 UPX.txt 日志。
使用 `--best --lzma` 并运行 `upx -t`；不支持的格式或校验失败会明确记录并跳过，
不强制加壳、不以未压缩文件冒充 UPX 版。若两版都不支持，Artifact 只包含报告。
参考 [UPX 官方说明](https://github.com/upx/upx/blob/devel/doc/upx-doc.txt)。

### 本地测量与验证（2026-09-23）

Go 1.27.0，Linux amd64，CGO=0，trimpath 和 stripped ldflags，
UPX 4.2.4 的 `--best --lzma`：

| 版本 | 原始可执行文件 | UPX 后可执行文件 |
| --- | ---: | ---: |
| 完整版基准 | 129937532 字节（123.9 MiB） | 24306756 字节（23.2 MiB） |
| Minimal | 46305404 字节（44.2 MiB） | 12087272 字节（11.5 MiB） |

Minimal 的普通 gzip 为 16743922 字节（16.0 MiB）；UPX 文件再次 gzip
为 12089655 字节（11.5 MiB）。这不是包含 geodata/配置文件的 Release ZIP 大小。
UPX 可明显减少磁盘占用，但不应从此推断内存占用或运行性能。

两版压缩副本均通过 `upx -t`，解压后 `cmp` 和 SHA-256 与原文件一致。
本机为 macOS，未运行加壳后的 Linux 二进制；尚未完成目标平台启动、
实际代理流量和进程管理兼容性验证，因此 UPX 只作为额外的实验 Artifact。

Minimal 已通过 Linux amd64、Windows amd64/386、Android arm64 交叉编译；
全仓库测试包编译（不运行外部服务测试）通过；完整/minimal 两配置的证书相关
单元测试和 Hysteria2 基础连接（含 Salamander）回归通过。
大 UDP 包和端口跳跃的既有问题仍以升级评审文档为准。
