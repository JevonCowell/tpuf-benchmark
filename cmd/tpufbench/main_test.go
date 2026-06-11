package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/turbopuffer/tpuf-benchmark/pkg/bench"
)

func newEnvFallbackTestFlagSet(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("workers", 1, "")
	flags.Bool("verbose", false, "")
	flags.Duration("timeout", time.Second, "")
	flags.String("name", "default", "")
	flags.StringArray("header", nil, "")

	if err := flags.Parse(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return flags
}

func TestApplyEnvFallbacksExplicitFlagWinsOverEnv(t *testing.T) {
	t.Setenv("TPUF_TEST_WORKERS", "99")

	flags := newEnvFallbackTestFlagSet(t, "--workers=7")
	err := applyEnvFallbacks([]envFallback{{FlagName: "workers", EnvName: "TPUF_TEST_WORKERS"}}, flags)
	if err != nil {
		t.Fatalf("applyEnvFallbacks: %v", err)
	}

	got, err := flags.GetInt("workers")
	if err != nil {
		t.Fatalf("GetInt: %v", err)
	}
	if got != 7 {
		t.Fatalf("workers = %d, want explicit flag value 7", got)
	}
}

func TestApplyEnvFallbacksEnvAppliesWhenFlagAbsent(t *testing.T) {
	t.Setenv("TPUF_TEST_WORKERS", "42")

	flags := newEnvFallbackTestFlagSet(t)
	err := applyEnvFallbacks([]envFallback{{FlagName: "workers", EnvName: "TPUF_TEST_WORKERS"}}, flags)
	if err != nil {
		t.Fatalf("applyEnvFallbacks: %v", err)
	}

	got, err := flags.GetInt("workers")
	if err != nil {
		t.Fatalf("GetInt: %v", err)
	}
	if got != 42 {
		t.Fatalf("workers = %d, want env value 42", got)
	}
}

func TestApplyEnvFallbacksEmptyEnvIgnored(t *testing.T) {
	t.Setenv("TPUF_TEST_NAME", "")

	flags := newEnvFallbackTestFlagSet(t)
	err := applyEnvFallbacks([]envFallback{{FlagName: "name", EnvName: "TPUF_TEST_NAME"}}, flags)
	if err != nil {
		t.Fatalf("applyEnvFallbacks: %v", err)
	}

	got, err := flags.GetString("name")
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if got != "default" {
		t.Fatalf("name = %q, want default", got)
	}
}

func TestApplyEnvFallbacksInvalidEnvValuesReturnErrors(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		envName  string
		envValue string
	}{
		{
			name:     "invalid int",
			flagName: "workers",
			envName:  "TPUF_TEST_WORKERS",
			envValue: "not-an-int",
		},
		{
			name:     "invalid bool",
			flagName: "verbose",
			envName:  "TPUF_TEST_VERBOSE",
			envValue: "not-a-bool",
		},
		{
			name:     "invalid duration",
			flagName: "timeout",
			envName:  "TPUF_TEST_TIMEOUT",
			envValue: "not-a-duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envName, tt.envValue)

			flags := newEnvFallbackTestFlagSet(t)
			err := applyEnvFallbacks([]envFallback{{FlagName: tt.flagName, EnvName: tt.envName}}, flags)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.envName) {
				t.Fatalf("error %q does not mention env var %s", err, tt.envName)
			}
		})
	}
}

func TestApplyEnvFallbacksStringArrayFlag(t *testing.T) {
	t.Setenv("TPUF_TEST_HEADERS", "X-One=1,X-Two=2")

	flags := newEnvFallbackTestFlagSet(t)
	err := applyEnvFallbacks([]envFallback{{FlagName: "header", EnvName: "TPUF_TEST_HEADERS"}}, flags)
	if err != nil {
		t.Fatalf("applyEnvFallbacks: %v", err)
	}

	got, err := flags.GetStringArray("header")
	if err != nil {
		t.Fatalf("GetStringArray: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"X-One=1,X-Two=2"}) {
		t.Fatalf("header = %#v, want env value", got)
	}
}

func TestExistingNamespacesProvided(t *testing.T) {
	flags := newEnvFallbackTestFlagSet(t)
	if existingNamespacesProvided(flags) {
		t.Fatal("existingNamespacesProvided = true, want false")
	}

	flags = newEnvFallbackTestFlagSet(t)
	flags.StringArray("existing-namespaces", nil, "")
	if err := flags.Set("existing-namespaces", ""); err != nil {
		t.Fatalf("set existing-namespaces: %v", err)
	}
	if !existingNamespacesProvided(flags) {
		t.Fatal("existingNamespacesProvided = false for explicitly set flag")
	}

	t.Setenv(envExistingNamespaces, "")
	flags = newEnvFallbackTestFlagSet(t)
	if !existingNamespacesProvided(flags) {
		t.Fatal("existingNamespacesProvided = false for env var")
	}
}

func TestParseListValues(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    []string
		wantErr bool
	}{
		{
			name:   "empty",
			values: nil,
			want:   nil,
		},
		{
			name:   "blank string",
			values: []string{"   "},
			want:   nil,
		},
		{
			name:   "comma separated",
			values: []string{"ns1,ns2,ns3"},
			want:   []string{"ns1", "ns2", "ns3"},
		},
		{
			name:   "newline separated",
			values: []string{"ns1\nns2\r\nns3"},
			want:   []string{"ns1", "ns2", "ns3"},
		},
		{
			name:   "repeated values",
			values: []string{"ns1", "ns2, ns3"},
			want:   []string{"ns1", "ns2", "ns3"},
		},
		{
			name:   "ignores empty entries",
			values: []string{" ns1,, ,ns2 "},
			want:   []string{"ns1", "ns2"},
		},
		{
			name:    "rejects duplicates",
			values:  []string{"ns1,ns2,ns1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseListValues(tt.values)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseListValues: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("values = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseHeaders(t *testing.T) {
	tests := []struct {
		name  string
		specs []string
		want  []bench.Header
	}{
		{
			name:  "equals separator",
			specs: []string{"X-Test=abc"},
			want:  []bench.Header{{Name: "X-Test", Value: "abc"}},
		},
		{
			name:  "colon separator",
			specs: []string{"X-Test: abc"},
			want:  []bench.Header{{Name: "X-Test", Value: "abc"}},
		},
		{
			name:  "value contains separator",
			specs: []string{"Authorization=Bearer a=b:c"},
			want:  []bench.Header{{Name: "Authorization", Value: "Bearer a=b:c"}},
		},
		{
			name:  "env-style list",
			specs: []string{"X-One=1,X-Two=2\nX-Three: 3"},
			want: []bench.Header{
				{Name: "X-One", Value: "1"},
				{Name: "X-Two", Value: "2"},
				{Name: "X-Three", Value: "3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeaders(tt.specs)
			if err != nil {
				t.Fatalf("parseHeaders: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("headers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseHeadersInvalid(t *testing.T) {
	tests := []string{
		"missing-separator",
		"=missing-name",
		"bad name=value",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			_, err := parseHeaders([]string{tt})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt) {
				t.Fatalf("error %q does not mention header %q", err, tt)
			}
		})
	}
}
