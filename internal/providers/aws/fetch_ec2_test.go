package aws

import (
	"context"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestFetchInstancesReturnsPartialAPIFailure(t *testing.T) {
	client := ec2.NewFromConfig(sdkaws.Config{Region: "us-east-2"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resources, errs := fetchInstances(ctx, client, "us-east-2", nil)
	if len(resources) != 0 {
		t.Fatalf("expected no resources after canceled request, got %d", len(resources))
	}
	if len(errs) != 1 {
		t.Fatalf("expected API error to be retained, got %v", errs)
	}
}

func TestResourceScopeDoesNotEnableSecurityGroups(t *testing.T) {
	scope := []string{"aws_s3_bucket", "aws_instance", "aws_vpc", "aws_subnet"}
	if allowedType(scope, "aws_security_group") {
		t.Fatal("security group fetch should be skipped when excluded from scope")
	}
	for _, resourceType := range scope {
		if !allowedType(scope, resourceType) {
			t.Errorf("configured resource type %q should remain enabled", resourceType)
		}
	}
}
