package executor

import "testing"

func TestSecurityValidatorRejectsDockerSocket(t *testing.T) {
	validator, err := NewSecurityValidator([]string{"/data"})
	if err != nil {
		t.Fatal(err)
	}
	model := map[string]any{"services": map[string]any{
		"trainer": map[string]any{"volumes": []any{map[string]any{
			"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock",
		}}},
	}}
	if err := validator.ValidateCompose(model); err == nil {
		t.Fatal("expected Docker socket rejection")
	}
}
