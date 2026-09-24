# 自定义 SOCKS5 / Hysteria2 连接故障复现（2026-09-24）

## 确认的原因

1. 提供的服务器日志显示：DispatchLink 将 buf.BufferedReader 强制转换为
   *pipe.Reader，引发 panic，进程以 status=2 退出；systemd 随后重启。
   这是进程级崩溃，会一起关闭三个 SOCKS5 监听。连接可能重置，重启空窗
   会拒绝新连接。这不是 WARP/TW 同时不可用的证据。
   历史代码中的这一断言已在 465c46a 中移除；当前分支使用通用 buf.Reader。
   日志中的 cert monitor 启动文本也不是当前实现。应核对实际运行二进制，
   不要仅凭 XrayR 0.9.5 横幅判断版本，因为 version 字符串仍是固定值。
2. 当前实现另有静态用户误判：custom_inbound.json 的 HY2 用户没有面板
   limiter 记录，却被 DispatchLink 的 UUID/限速/授权校验拒绝。
   修复前隔离测试中三个 SOCKS5 正常，静态 HY2 的 HTTP 请求稳定返回 EOF；
   修复后四个入站均成功传输。
3. 配置副本的 HY2 端口为 55444，分享链接为 55443；用户确认前者为临时避让，
   实际测试监听的是 55443，因此不将端口差异认定为本次原因。

## 修复边界

- panel 在 core 启动之前明确登记本地自定义入站 tag。只有这些 tag 跳过
  面板用户授权和限速包装；协议原生认证及原生用户统计仍保留。
- 不是“查不到用户就放行”：未知 tag、已删除面板用户和面板管理节点继续
  默认拒绝，保留面板计费、动态限速和停用机制。
- 自定义入站与面板节点 tag 冲突时拒绝接管；面板清理/回滚也不得移除
  本地自定义入站。静态用户由本地配置维护，不自动享有面板计费与停用控制。
- 未修改 test/ 内的配置、证书路径、端口或凭据，也未连接真实面板、WARP/TW。
  test/ 和 .DS_Store 已加入忽略规则，不随修复提交。

## 验证方法与结果

测试读取本地配置副本，只替换绑定端口、测试证书和真实出站实现。
WARP/TW 被同 tag 的本机 HTTP 出站替身替代；保留入站认证、嗅探和路由规则。
请求目标使用公开 IP 形式，但由 freedom redirect 转发至本机，不访问公网。

- 三个 SOCKS5：Go 客户端及真实 curl 均分别得到默认、warp-out、tw-out
  替身响应，证明入站与路由关联可用，不证明真实代理链路可用。
- 静态 HY2：修复前 EOF，修复后实际 QUIC/TLS 连接及 HTTP 数据转发通过。
- 私网拦截规则优先于 SOCKS inboundTag 路由。访问私网目标时，在监听正常的
  情况下实测可出现 curl 52/56；这是错误类别复现，不能据此解释用户访问
  ip.sb 的全部失败。未改动这条安全规则。
- core 关闭时实测三个 SOCKS5 端口拒绝连接，重启后恢复监听，与服务器日志
  所显示的崩溃/重启过程一致。
- 通用配置连续 10 次四入站测试通过；BufferedReader 嗅探、限速器及授权
  单元测试通过 race 检查；原面板 HY2 基础计费/限速/停用回归通过。
- 全链路 race 测试还发现固定版本 core 中 udpSessionManager.clean/run
  对 closed 的并发读写（transport/internet/hysteria/conn.go）。本次未改 core，
  该问题需单独修复，不能声称完整 race 检查通过。

可复现命令：

```sh
go test -tags nolego ./panel -run '^TestCustomInboundConnectivity$' -count=10
XRAYR_TEST_CONFIG_DIR=/absolute/path/to/local/test go test -tags nolego ./panel -run '^TestCustomInboundConnectivity$' -v
go test -race ./app/mydispatcher ./common/limiter
```

## 服务器验证建议

1. 从包含本次修复的分支重新构建/下载，核对 systemd 的 ExecStart、MainPID
   和实际可执行文件哈希，避免覆盖了一个文件但服务仍运行另一个旧文件。
   使用 systemctl show XrayR -p ExecStart -p MainPID -p NRestarts 检查启动目标。
2. 重启后观察 journalctl -u XrayR -f，确认不再出现 BufferedReader/pipe.Reader
   panic，且 NRestarts 不再增加。需要排障时临时将 Log.Level 改为 warning/debug；
   分享日志前隐藏认证、用户和面板信息。
3. 在同一网络命名空间依次测试 socks5h://127.0.0.1:1234、1081、1082，
   然后连接 HY2，再重复 SOCKS5 测试，确认 HY2 请求不再击穿整个进程。
4. 确认实际 UDP 55443、SNI 对应证书和认证密码一致。该环境未持有生产证书，
   未验证公网 UDP 防火墙、生产证书链或真实 WARP/TW；若进程稳定后仍有
   单一路由错误，须结合请求时间点的出站日志继续定位。
