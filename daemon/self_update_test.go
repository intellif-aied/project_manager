package main

import "testing"

func TestVersionGreaterDoesNotDowngrade(t *testing.T) {
	for _, test := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"0.1.5", "0.1.4", true},
		{"0.2.0", "0.1.99", true},
		{"0.1.4", "0.1.4", false},
		{"0.1.3", "0.1.4", false},
	} {
		if got := versionGreater(test.candidate, test.current); got != test.want {
			t.Fatalf("versionGreater(%q,%q)=%t want %t", test.candidate, test.current, got, test.want)
		}
	}
}

func TestChecksumForFile(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	value, err := checksumForFile([]byte(hash+"  aida-linux-amd64\n"), "aida-linux-amd64")
	if err != nil || value != hash {
		t.Fatalf("value=%q err=%v", value, err)
	}
}
