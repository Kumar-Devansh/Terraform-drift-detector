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

func TestEngineDoesNotReportManagedResourceAsExtra(t *testing.T) {
	resource := model.Resource{ID: "aws/aws_instance/i-managed", Type: "aws_instance", CloudID: "i-managed"}
	findings := NewEngine(model.CompareConfig{}).Compare([]model.Resource{resource}, []model.Resource{resource})
	for _, finding := range findings {
		if finding.Kind == model.DriftExtraInCloud {
			t.Fatalf("managed resource was reported as extra: %+v", finding)
		}
	}
}

func TestEngineReportsMultipleExtraResources(t *testing.T) {
	expected := []model.Resource{{ID: "aws/aws_instance/i-managed", Type: "aws_instance", CloudID: "i-managed"}}
	actual := append([]model.Resource{}, expected...)
	actual = append(actual,
		model.Resource{ID: "aws/aws_instance/i-extra-1", Type: "aws_instance", CloudID: "i-extra-1"},
		model.Resource{ID: "aws/aws_instance/i-extra-2", Type: "aws_instance", CloudID: "i-extra-2"},
	)
	findings := NewEngine(model.CompareConfig{}).Compare(expected, actual)
	if got := BuildSummary(len(expected), findings).ExtraInCloud; got != 2 {
		t.Fatalf("expected 2 extra resources, got %d (findings: %+v)", got, findings)
	}
}

func TestS3VersioningMatchesAWSRepresentation(t *testing.T) {
	expected := model.Resource{ID: "aws/aws_s3_bucket/example", Type: "aws_s3_bucket", Attributes: map[string]any{
		"versioning": map[string]any{"enabled": true, "mfa_delete": false},
	}}
	actual := model.Resource{ID: expected.ID, Type: expected.Type, Attributes: map[string]any{
		"versioning": map[string]any{"enabled": true, "mfa_delete": false},
	}}
	if findings := NewEngine(model.CompareConfig{}).Compare([]model.Resource{expected}, []model.Resource{actual}); len(findings) != 0 {
		t.Fatalf("matching S3 versioning produced findings: %+v", findings)
	}
}

func TestS3VersioningChangeIsReported(t *testing.T) {
	expected := model.Resource{ID: "aws/aws_s3_bucket/example", Type: "aws_s3_bucket", Attributes: map[string]any{
		"versioning": map[string]any{"enabled": true, "mfa_delete": false},
	}}
	actual := model.Resource{ID: expected.ID, Type: expected.Type, Attributes: map[string]any{
		"versioning": map[string]any{"enabled": false, "mfa_delete": false},
	}}
	findings := NewEngine(model.CompareConfig{}).Compare([]model.Resource{expected}, []model.Resource{actual})
	if len(findings) != 1 || findings[0].Field != "versioning" || findings[0].Kind != model.DriftAttributeChange {
		t.Fatalf("expected one versioning drift finding, got %+v", findings)
	}
}

func TestS3UnavailableAndTerraformOnlyAttributesAreIgnored(t *testing.T) {
	expected := model.Resource{ID: "aws/aws_s3_bucket/example", Type: "aws_s3_bucket", Attributes: map[string]any{
		"force_destroy": true,
		"versioning":    map[string]any{"enabled": true},
		"acl":           "private",
	}}
	actual := model.Resource{ID: expected.ID, Type: expected.Type, Attributes: map[string]any{
		"versioning": nil,
	}}
	if findings := NewEngine(model.CompareConfig{}).Compare([]model.Resource{expected}, []model.Resource{actual}); len(findings) != 0 {
		t.Fatalf("unavailable or Terraform-only S3 attributes produced findings: %+v", findings)
	}
}
