package netbird

import "testing"

func TestParse(t *testing.T) {
	s, e := Parse([]byte(`{"netbirdIp":"100.1.2.3/16","management":{"connected":true},"signal":{"connected":false},"peers":{"total":3,"connected":2}}`))
	if e != nil || s[0].Value != "OFF" || s[2].Value != 2 {
		t.Fatalf("%v %v", s, e)
	}
	for _, data := range []string{`{`, `{}`} {
		if _, e = Parse([]byte(data)); e == nil {
			t.Fatal("expected error")
		}
	}
}
