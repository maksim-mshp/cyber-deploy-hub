package checker

import "testing"

func TestCommandForStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step Step
		want string
	}{
		{
			name: "file exists quotes path",
			step: Step{Type: StepFileExists, Path: "/tmp/a file"},
			want: "test -e '/tmp/a file'",
		},
		{
			name: "file contains quotes pattern",
			step: Step{Type: StepFileContains, Path: "/etc/app.conf", Contains: "key='value'"},
			want: "grep -F -- 'key='\"'\"'value'\"'\"'' '/etc/app.conf' >/dev/null",
		},
		{
			name: "command is direct profile command",
			step: Step{Type: StepCommandExitCode, Command: "test -r /etc/os-release"},
			want: "test -r /etc/os-release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := commandForStep(tt.step)
			if err != nil {
				t.Fatalf("commandForStep: %v", err)
			}
			if got != tt.want {
				t.Fatalf("command = %q, want %q", got, tt.want)
			}
		})
	}
}
