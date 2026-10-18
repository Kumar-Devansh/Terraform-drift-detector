package state

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/driftctl/driftctl/internal/model"
)

func readS3(ctx context.Context, cfg model.StateConfig) ([]byte, error) {
	if cfg.Bucket == "" || cfg.Key == "" {
		return nil, fmt.Errorf("s3 backend requires bucket and key")
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(cfg.Key),
	})
	if err != nil {
		return nil, fmt.Errorf("get s3 object s3://%s/%s: %w", cfg.Bucket, cfg.Key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}
