package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/driftctl/driftctl/internal/model"
)

func fetchBuckets(ctx context.Context, client *s3.Client, region string, expected []model.Resource) ([]model.Resource, []error) {
	var resources []model.Resource
	var errs []error

	paginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{})
	for paginator.HasMorePages() {
		listed, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, []error{fmt.Errorf("list buckets: %w", err)}
		}
		for _, b := range listed.Buckets {
			bucket := aws.ToString(b.Name)
			// S3 bucket names don't include arn prefix
			if len(bucket) > 5 && bucket[:5] == "arn:" {
				// extract bucket from arn:aws:s3:::bucket-name
				parts := splitARN(bucket)
				if len(parts) > 0 {
					bucket = parts[len(parts)-1]
				}
			}

			locOut, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
				Bucket: aws.String(bucket),
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("bucket %s: %w", bucket, err))
				continue
			}

			bucketRegion := string(locOut.LocationConstraint)
			if bucketRegion == "" {
				bucketRegion = "us-east-1"
			}
			if bucketRegion == "EU" {
				bucketRegion = "eu-west-1"
			}
			if bucketRegion != region {
				continue
			}

			tags := map[string]string{}
			tagsOut, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
				Bucket: aws.String(bucket),
			})
			if err == nil {
				for _, t := range tagsOut.TagSet {
					tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}

			attrs := map[string]any{}

			verOut, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
				Bucket: aws.String(bucket),
			})
			if err == nil {
				attrs["versioning"] = map[string]any{
					"enabled":    verOut.Status == "Enabled",
					"mfa_delete": verOut.MFADelete == "Enabled",
				}
			}

			resources = append(resources, baseResource("aws_s3_bucket", bucket, bucket, bucketRegion, attrs, tags))
		}
	}
	return resources, errs
}

func splitARN(arn string) []string {
	var parts []string
	current := ""
	for _, c := range arn {
		if c == ':' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
