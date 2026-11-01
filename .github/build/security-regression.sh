#!/usr/bin/env bash
set -euo pipefail

case "${EDITION:-full}" in
  full) tags="" ;;
  minimal|nolego) tags="$EDITION" ;;
  *) echo "Unsupported edition: $EDITION" >&2; exit 1 ;;
esac

# All fixtures are local/mock data. Never run the legacy live-panel or ACME tests.
go test -mod=readonly -tags "$tags" \
  ./common/mylego ./common/limiter ./common/porthopping \
  ./api/sspanel ./service/controller ./app/mydispatcher ./panel \
  -run '^(TestMinimal.*|TestCertMonitor.*|TestLazy.*|TestStaticInbound.*|TestReaddedUser.*|TestRemovedUser.*|TestExistingConnection.*|TestBufferedReader.*|TestSniffingTimeout.*|TestStatsWriter|TestCustom.*|TestParseCustom.*|TestPanel.*|TestParsePorts.*|TestFirewallFamilies|TestDualStack.*|TestConcreteListener.*|TestInvalidConfiguration.*|TestResolveConfigPrecedence|TestGetNodeInfoHysteria2Contract|TestParseDisabledNode|TestSubtractTrafficAfterIdempotentRetry|TestKeepConnectUserLimitWinsOverLocalNodeLimit|TestTrafficMultipleFailuresDoNotDoubleBill|TestNodeConfigPortForms|TestNodeAndUsersAlwaysRefetchedForRecovery|Test_merge|Test_needRenewal|TestHysteriaBasicConnectivity|TestHysteriaLifecycleAndTCPForwarding)$' \
  -count=1 -timeout=120s

# Protocol-level large datagrams are independent of host UDP size limits.
go test -mod=readonly \
  github.com/xtls/xray-core/proxy/hysteria \
  github.com/xtls/xray-core/transport/internet/finalmask/udphop \
  github.com/xtls/xray-core/transport/internet \
  -run '^(TestUDPRoundTripLargeDatagrams|TestUDPReaderShortBuffer|TestDefaultListenerDisablesMPTCP|Test.*RemoteOnly.*)$' \
  -count=1 -timeout=90s

# CGO is enabled only for the race-instrumented test, never release binaries.
CGO_ENABLED=1 go test -mod=readonly -race \
  github.com/xtls/xray-core/transport/internet/tls \
  -run '^TestCertificateSnapshot.*$' -count=1 -timeout=90s
