package config

import (
	"os"
	"testing"
)

func TestGetWorkerCount(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		raw  string
		want int
		ok   bool
	}{
		{name: "absent uses default", want: DefaultWorkerCount, ok: true},
		{name: "explicit twenty", set: true, raw: "20", want: 20, ok: true},
		{name: "empty is invalid", set: true, raw: "", ok: false},
		{name: "zero is invalid", set: true, raw: "0", ok: false},
		{name: "too large is invalid", set: true, raw: "257", ok: false},
		{name: "text is invalid", set: true, raw: "many", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const key = "TEST_WORKER_COUNT"
			if test.set {
				t.Setenv(key, test.raw)
			} else {
				t.Setenv(key, "temporary")
				t.Setenv(key, "")
				// Setenv cannot represent absence.
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			got, err := getWorkerCount(key)
			if (err == nil) != test.ok {
				t.Fatalf("error = %v, want success %v", err, test.ok)
			}
			if test.ok && got != test.want {
				t.Fatalf("count = %d, want %d", got, test.want)
			}
		})
	}
}

func TestLoadWorkerCountsUsesIndependentSettings(t *testing.T) {
	t.Setenv("REPORT_RUN_PROCESSOR_COUNT", "21")
	t.Setenv("DIGEST_BACKGROUND_WORKER_COUNT", "22")
	t.Setenv("DIGEST_INTERACTIVE_WORKER_COUNT", "23")
	counts, err := LoadWorkerCounts()
	if err != nil {
		t.Fatal(err)
	}
	if counts.ReportRun != 21 || counts.DigestBackground != 22 || counts.DigestInteractive != 23 {
		t.Fatalf("counts = %#v", counts)
	}
}
