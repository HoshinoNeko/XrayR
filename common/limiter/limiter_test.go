package limiter

import (
	"sync"
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
	"github.com/xtls/xray-core/common/buf"
)

func TestLazyBucketConcurrentReuse(t *testing.T) {
	l := New()
	users := []api.UserInfo{{UID: 1, DeviceLimit: 1}}
	if err := l.AddInboundLimiter("node", 125000, &users, nil); err != nil {
		t.Fatal(err)
	}
	first, _, reject := l.GetUserBucket("node", "node||1", "192.0.2.1")
	if reject || first == nil {
		t.Fatal("initial bucket missing")
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bucket, limited, reject := l.GetUserBucket("node", "node||1", "192.0.2.1")
			if reject || !limited || bucket != first {
				t.Error("cached bucket not reused")
			}
		}()
	}
	wg.Wait()
	if _, _, reject := l.GetUserBucket("node", "node||1", "192.0.2.2"); !reject {
		t.Fatal("device limit bypassed")
	}
}

func BenchmarkGetUserBucketCached(b *testing.B) {
	l := New()
	users := []api.UserInfo{{UID: 1}}
	if err := l.AddInboundLimiter("node", 125000, &users, nil); err != nil {
		b.Fatal(err)
	}
	l.GetUserBucket("node", "node||1", "192.0.2.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.GetUserBucket("node", "node||1", "192.0.2.1")
	}
}

func TestReaddedUserDoesNotReviveOldAuthorization(t *testing.T) {
	l := New()
	users := []api.UserInfo{{UID: 1, UUID: "old"}}
	if err := l.AddInboundLimiter("node", 0, &users, nil); err != nil {
		t.Fatal(err)
	}
	allowed := l.Permission("node", "node||1")
	if !allowed() || !l.MatchesUUID("node", "node||1", "old") {
		t.Fatal("initial authorization missing")
	}
	if err := l.RemoveInboundUsers("node", []string{"node||1"}); err != nil {
		t.Fatal(err)
	}
	users[0].UUID = "new"
	if err := l.UpdateInboundLimiter("node", &users); err != nil {
		t.Fatal(err)
	}
	if allowed() || l.MatchesUUID("node", "node||1", "old") || !l.Permission("node", "node||1")() {
		t.Fatal("old authorization revived or new authorization rejected")
	}
}

func TestRemovedUserIsRejected(t *testing.T) {
	l := New()
	users := []api.UserInfo{{UID: 1, Email: "user@example.com"}}
	userTag := "Hysteria2_0.0.0.0_443|user@example.com|1"
	if err := l.AddInboundLimiter("Hysteria2_0.0.0.0_443", 0, &users, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, reject := l.GetUserBucket("Hysteria2_0.0.0.0_443", userTag, "192.0.2.1"); reject {
		t.Fatal("authorized user was rejected")
	}
	if err := l.RemoveInboundUsers("Hysteria2_0.0.0.0_443", []string{userTag}); err != nil {
		t.Fatal(err)
	}
	if _, _, reject := l.GetUserBucket("Hysteria2_0.0.0.0_443", userTag, "192.0.2.1"); !reject {
		t.Fatal("removed user was not rejected")
	}
}

func TestExistingConnectionFollowsPanelLimitAndRevocation(t *testing.T) {
	l := New()
	users := []api.UserInfo{{UID: 1}}
	if err := l.AddInboundLimiter("node", 12500000, &users, nil); err != nil {
		t.Fatal(err)
	}
	writer := l.UserWriter(buf.Discard, "node", "node||1", "192.0.2.1")
	if err := writer.WriteMultiBuffer(buf.MergeBytes(nil, []byte("before"))); err != nil {
		t.Fatal(err)
	}
	users[0].SpeedLimit = 125000
	if err := l.UpdateInboundLimiter("node", &users); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteMultiBuffer(buf.MergeBytes(nil, []byte("after"))); err != nil {
		t.Fatal(err)
	}
	bucket, _, reject := l.GetUserBucket("node", "node||1", "192.0.2.1")
	if reject || bucket.Limit() != 125000 {
		t.Fatal("1 Mbps not applied")
	}
	l.RemoveInboundUsers("node", []string{"node||1"})
	if err := writer.WriteMultiBuffer(buf.MergeBytes(nil, []byte("revoked"))); err == nil {
		t.Fatal("revoked connection still writable")
	}
}
