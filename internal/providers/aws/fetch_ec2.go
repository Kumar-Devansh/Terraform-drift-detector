package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/driftctl/driftctl/internal/model"
)

func fetchInstances(ctx context.Context, client *ec2.Client, region string, expected []model.Resource) ([]model.Resource, []error) {
	var resources []model.Resource
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, []error{fmt.Errorf("describe instances: %w", err)}
		}
		for _, res := range out.Reservations {
			for _, inst := range res.Instances {
				id := aws.ToString(inst.InstanceId)

				sgIDs := make([]string, 0, len(inst.SecurityGroups))
				for _, sg := range inst.SecurityGroups {
					sgIDs = append(sgIDs, aws.ToString(sg.GroupId))
				}

				tags := ec2TagMap(inst.Tags)
				name := tags["Name"]
				if name == "" {
					name = id
				}

				attrs := map[string]any{
					"instance_type":          string(inst.InstanceType),
					"ami":                    aws.ToString(inst.ImageId),
					"vpc_security_group_ids": sgIDs,
					"subnet_id":              aws.ToString(inst.SubnetId),
					"monitoring":             inst.Monitoring != nil && inst.Monitoring.State == ec2types.MonitoringStateEnabled,
				}

				resources = append(resources, baseResource("aws_instance", id, name, region, attrs, tags))
			}
		}
	}
	return resources, nil
}

func fetchVPCs(ctx context.Context, client *ec2.Client, region string, expected []model.Resource) ([]model.Resource, []error) {
	var resources []model.Resource
	paginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, []error{fmt.Errorf("describe vpcs: %w", err)}
		}
		for _, vpc := range out.Vpcs {
			id := aws.ToString(vpc.VpcId)
			tags := ec2TagMap(vpc.Tags)
			name := tags["Name"]
			if name == "" {
				name = id
			}
			attrs := map[string]any{
				"cidr_block":           aws.ToString(vpc.CidrBlock),
				"enable_dns_hostnames": nil,
				"enable_dns_support":   nil,
				"instance_tenancy":     string(vpc.InstanceTenancy),
			}
			resources = append(resources, baseResource("aws_vpc", id, name, region, attrs, tags))
		}
	}
	return resources, nil
}

func fetchSubnets(ctx context.Context, client *ec2.Client, region string, expected []model.Resource) ([]model.Resource, []error) {
	var resources []model.Resource
	paginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, []error{fmt.Errorf("describe subnets: %w", err)}
		}
		for _, subnet := range out.Subnets {
			id := aws.ToString(subnet.SubnetId)
			tags := ec2TagMap(subnet.Tags)
			name := tags["Name"]
			if name == "" {
				name = id
			}
			attrs := map[string]any{
				"cidr_block":              aws.ToString(subnet.CidrBlock),
				"vpc_id":                  aws.ToString(subnet.VpcId),
				"map_public_ip_on_launch": subnet.MapPublicIpOnLaunch,
				"availability_zone":       aws.ToString(subnet.AvailabilityZone),
			}
			resources = append(resources, baseResource("aws_subnet", id, name, region, attrs, tags))
		}
	}
	return resources, nil
}

func fetchSecurityGroups(ctx context.Context, client *ec2.Client, region string, expected []model.Resource) ([]model.Resource, []error) {
	var resources []model.Resource
	paginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, []error{fmt.Errorf("describe security groups: %w", err)}
		}
		for _, sg := range out.SecurityGroups {
			id := aws.ToString(sg.GroupId)
			tags := ec2TagMap(sg.Tags)
			name := aws.ToString(sg.GroupName)
			attrs := map[string]any{
				"description": aws.ToString(sg.Description),
				"vpc_id":      aws.ToString(sg.VpcId),
				"ingress":     normalizeRules(sg.IpPermissions),
				"egress":      normalizeRules(sg.IpPermissionsEgress),
			}
			resources = append(resources, baseResource("aws_security_group", id, name, region, attrs, tags))
		}
	}
	return resources, nil
}

func ec2TagMap(tags []ec2types.Tag) map[string]string {
	out := make(map[string]string)
	for _, t := range tags {
		out[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return out
}

func normalizeRules(perms []ec2types.IpPermission) []map[string]any {
	var rules []map[string]any
	for _, p := range perms {
		rule := map[string]any{
			"protocol":  aws.ToString(p.IpProtocol),
			"from_port": p.FromPort,
			"to_port":   p.ToPort,
		}
		var cidrs []string
		for _, r := range p.IpRanges {
			cidrs = append(cidrs, aws.ToString(r.CidrIp))
		}
		if len(cidrs) > 0 {
			rule["cidr_blocks"] = cidrs
		}
		rules = append(rules, rule)
	}
	return rules
}
