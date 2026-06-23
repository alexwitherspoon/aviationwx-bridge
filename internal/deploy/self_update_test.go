package deploy

import "testing"

func TestSelfUpdateEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "unset defaults false", env: "", want: false},
		{name: "false string", env: "false", want: false},
		{name: "zero", env: "0", want: false},
		{name: "true string", env: "true", want: true},
		{name: "one", env: "1", want: true},
		{name: "invalid is false", env: "yes", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				t.Setenv(selfUpdateEnv, "")
			} else {
				t.Setenv(selfUpdateEnv, tt.env)
			}
			if got := SelfUpdateEnabled(); got != tt.want {
				t.Fatalf("SelfUpdateEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
