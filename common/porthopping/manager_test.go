package porthopping

import "testing"

func TestParsePorts(t *testing.T) {
	got, err := parsePorts("20000-20002, 443,30000-30100")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"20000:20002", "443", "30000:30100"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParsePortsRejectsInvalid(t *testing.T) {
	for _, value := range []string{"", "0", "2-1", "65536", "1-2-3"} {
		if _, err := parsePorts(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}
