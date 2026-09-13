package limiter

import (
	"testing"

	"github.com/HoshinoNeko/XrayR/api"
)

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
