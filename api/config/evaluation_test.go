package config

import "testing"

func TestEvaluationRuntimeRequiresExplicitTestIdentity(t *testing.T) {
	t.Setenv("AIDA_EVALUATION_ENABLED", "true")
	t.Setenv("AIDA_ENVIRONMENT", "production")
	t.Setenv("AIDA_EVALUATION_INSTANCE_ID", "production")
	if err := Load().ValidateEvaluationRuntime(); err == nil {
		t.Fatal("expected production evaluation runtime to be rejected")
	}

	t.Setenv("AIDA_ENVIRONMENT", "test")
	t.Setenv("AIDA_EVALUATION_INSTANCE_ID", "")
	if err := Load().ValidateEvaluationRuntime(); err == nil {
		t.Fatal("expected missing instance id to be rejected")
	}

	t.Setenv("AIDA_EVALUATION_INSTANCE_ID", "isolated-evaluation-1")
	if err := Load().ValidateEvaluationRuntime(); err != nil {
		t.Fatalf("valid test evaluation runtime: %v", err)
	}
}

func TestEvaluationRuntimeIsDisabledByDefault(t *testing.T) {
	t.Setenv("AIDA_EVALUATION_ENABLED", "false")
	t.Setenv("AIDA_ENVIRONMENT", "production")
	if err := Load().ValidateEvaluationRuntime(); err != nil {
		t.Fatalf("disabled evaluation runtime should not affect production: %v", err)
	}
}
