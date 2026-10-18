package drift

import (
	"testing"

	"github.com/driftctl/driftctl/internal/model"
)

func TestEngineCompareMissingAndTags(t *testing.T) {
	engine := NewEngine(model.CompareConfig{})

	expected := []model.Resource{
		{
			ID: "aws/aws_instance/i-123", Provider: "aws", Type: "aws_instance",
			CloudID: "i-123", Name: "web",
			Attributes: map[string]any{"instance_type": "t3.micro"},
			Tags:       map[string]string{"env": "prod"},
		},
	}

	actual := []model.Resource{
		{
			ID: "aws/aws_instance/i-123", Provider: "aws", Type: "aws_instance",
			CloudID: "i-123", Name: "web",
			Attributes: map[string]any{"instance_type": "t3.small"},
			Tags:       map[string]string{"env": "staging"},
		},
		{
			ID: "aws/aws_vpc/vpc-orphan", Provider: "aws", Type: "aws_vpc",
			CloudID: "vpc-orphan", Name: "orphan",
		},
	}

	findings := engine.Compare(expected, actual)
	if len(findings) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(findings))
	}

	summary := BuildSummary(len(expected), findings)
	if summary.AttributeChanges < 1 {
		t.Fatal("expected attribute change")
	}
	if summary.TagChanges < 1 {
		t.Fatal("expected tag change")
	}
	if summary.ExtraInCloud < 1 {
		t.Fatal("expected extra in cloud")
	}
}

func TestEngineCompareMissingInCloud(t *testing.T) {
	engine := NewEngine(model.CompareConfig{})
	expected := []model.Resource{
		{ID: "aws/aws_instance/i-gone", Provider: "aws", Type: "aws_instance", CloudID: "i-gone", Tags: map[string]string{"env": "prod"}},
	}
	findings := engine.Compare(expected, nil)
	if len(findings) != 1 || findings[0].Kind != model.DriftMissingInCloud {
		t.Fatalf("expected missing finding, got %+v", findings)
	}
	if findings[0].Severity != model.SeverityCritical {
		t.Fatalf("expected critical severity, got %s", findings[0].Severity)
	}
}
