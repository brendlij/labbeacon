package metric

import "testing"

func TestKey(t *testing.T) {
	if Key("/mnt/a-b") == Key("/mnt/a_b") {
		t.Fatal("slug collision")
	}
	if Key("/") == "" || Key("/") != Key("/") {
		t.Fatal("unstable key")
	}
}
