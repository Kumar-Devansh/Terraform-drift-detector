package aws

import (
	"context"
	"fmt"
	"sync"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/driftctl/driftctl/internal/model"
)

// Provider implements AWS cloud resource fetching.
type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string { return "aws" }

func (p *Provider) SupportedTypes() []string {
	return []string{
		"aws_instance",
		"aws_s3_bucket",
		"aws_security_group",
		"aws_vpc",
		"aws_subnet",
	}
}

func (p *Provider) FetchResources(ctx context.Context, expected []model.Resource, regions, resourceTypes []string) ([]model.Resource, error) {
	regionSet := make(map[string]bool)
	for _, r := range regions {
		regionSet[r] = true
	}
	// An explicit region list defines the complete scan scope. When no regions
	// are configured, retain the legacy behavior of using regions in state.
	if len(regions) == 0 {
		for _, e := range expected {
			if e.Region != "" {
				regionSet[e.Region] = true
			}
		}
	}
	if len(regionSet) == 0 {
		regionSet["us-east-2"] = true
	}

	byType := groupByType(expected)
	if len(resourceTypes) > 0 {
		allowed := make(map[string]bool, len(resourceTypes))
		for _, resourceType := range resourceTypes {
			allowed[resourceType] = true
		}
		for resourceType := range byType {
			if !allowed[resourceType] {
				delete(byType, resourceType)
			}
		}
	}
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		resources []model.Resource
		errs      []error
	)

	for region := range regionSet {
		region := region
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("region %s: %w", region, err))
				mu.Unlock()
				return
			}

			ec2Client := ec2.NewFromConfig(cfg)
			s3Client := s3.NewFromConfig(cfg)

			var regionResources []model.Resource
			var regionErrs []error

			for _, fetcher := range []struct {
				typeName string
				fetch    func() ([]model.Resource, []error)
			}{
				{"aws_instance", func() ([]model.Resource, []error) {
					return fetchInstances(ctx, ec2Client, region, byType["aws_instance"])
				}},
				{"aws_vpc", func() ([]model.Resource, []error) { return fetchVPCs(ctx, ec2Client, region, byType["aws_vpc"]) }},
				{"aws_subnet", func() ([]model.Resource, []error) { return fetchSubnets(ctx, ec2Client, region, byType["aws_subnet"]) }},
				{"aws_security_group", func() ([]model.Resource, []error) {
					return fetchSecurityGroups(ctx, ec2Client, region, byType["aws_security_group"])
				}},
				{"aws_s3_bucket", func() ([]model.Resource, []error) {
					return fetchBuckets(ctx, s3Client, region, byType["aws_s3_bucket"])
				}},
			} {
				if len(resourceTypes) > 0 && !allowedType(resourceTypes, fetcher.typeName) {
					continue
				}
				r, e := fetcher.fetch()
				regionResources = append(regionResources, r...)
				regionErrs = append(regionErrs, e...)
			}

			mu.Lock()
			resources = append(resources, regionResources...)
			errs = append(errs, regionErrs...)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(errs) > 0 {
		return resources, fmt.Errorf("aws fetch partially failed: %v", errs[0])
	}

	return resources, nil
}

func allowedType(resourceTypes []string, resourceType string) bool {
	for _, allowed := range resourceTypes {
		if allowed == resourceType {
			return true
		}
	}
	return false
}

func groupByType(expected []model.Resource) map[string][]model.Resource {
	out := make(map[string][]model.Resource)
	for _, r := range expected {
		out[r.Type] = append(out[r.Type], r)
	}
	return out
}

func baseResource(resType, cloudID, name, region string, attrs map[string]any, tags map[string]string) model.Resource {
	return model.Resource{
		ID:         fmt.Sprintf("aws/%s/%s", resType, cloudID),
		Provider:   "aws",
		Type:       resType,
		CloudID:    cloudID,
		Name:       name,
		Attributes: attrs,
		Tags:       tags,
		Region:     region,
		Source:     "cloud",
	}
}
