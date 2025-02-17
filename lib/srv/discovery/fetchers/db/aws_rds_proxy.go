/*
Copyright 2022 Gravitational, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package db

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
	"github.com/gravitational/trace"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/cloud"
	libcloudaws "github.com/gravitational/teleport/lib/cloud/aws"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/srv/discovery/common"
)

// newRDSDBProxyFetcher returns a new AWS fetcher for RDS Proxy databases.
func newRDSDBProxyFetcher(cfg awsFetcherConfig) (common.Fetcher, error) {
	return newAWSFetcher(cfg, &rdsDBProxyPlugin{})
}

// rdsDBProxyPlugin retrieves RDS Proxies and their custom endpoints.
type rdsDBProxyPlugin struct{}

func (f *rdsDBProxyPlugin) ComponentShortName() string {
	return "rdsproxy"
}

// GetDatabases returns a list of database resources representing RDS
// Proxies and custom endpoints.
func (f *rdsDBProxyPlugin) GetDatabases(ctx context.Context, cfg *awsFetcherConfig) (types.Databases, error) {
	rdsClient, err := cfg.AWSClients.GetAWSRDSClient(ctx, cfg.Region,
		cloud.WithAssumeRole(cfg.AssumeRole.RoleARN, cfg.AssumeRole.ExternalID),
		cloud.WithCredentialsMaybeIntegration(cfg.Integration),
	)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// Get a list of all RDS Proxies. Each RDS Proxy has one "default"
	// endpoint.
	rdsProxies, err := getRDSProxies(ctx, rdsClient, maxAWSPages)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Get all RDS Proxy custom endpoints sorted by the name of the RDS Proxy
	// that owns the custom endpoints.
	customEndpointsByProxyName, err := getRDSProxyCustomEndpoints(ctx, rdsClient, maxAWSPages)
	if err != nil {
		cfg.Log.Debugf("Failed to get RDS Proxy endpoints: %v.", err)
	}

	var databases types.Databases
	for _, dbProxy := range rdsProxies {
		if !aws.BoolValue(dbProxy.RequireTLS) {
			cfg.Log.Debugf("RDS Proxy %q doesn't support TLS. Skipping.", aws.StringValue(dbProxy.DBProxyName))
			continue
		}

		if !services.IsRDSProxyAvailable(dbProxy) {
			cfg.Log.Debugf("The current status of RDS Proxy %q is %q. Skipping.",
				aws.StringValue(dbProxy.DBProxyName),
				aws.StringValue(dbProxy.Status))
			continue
		}

		// rds.DBProxy has no port information. An extra SDK call is made to
		// find the port from its targets.
		port, err := getRDSProxyTargetPort(ctx, rdsClient, dbProxy.DBProxyName)
		if err != nil {
			cfg.Log.Debugf("Failed to get port for RDS Proxy %v: %v.", aws.StringValue(dbProxy.DBProxyName), err)
			continue
		}

		// rds.DBProxy has no tags information. An extra SDK call is made to
		// fetch the tags. If failed, keep going without the tags.
		tags, err := listRDSResourceTags(ctx, rdsClient, dbProxy.DBProxyArn)
		if err != nil {
			cfg.Log.Debugf("Failed to get tags for RDS Proxy %v: %v.", aws.StringValue(dbProxy.DBProxyName), err)
		}

		// Add a database from RDS Proxy (default endpoint).
		database, err := services.NewDatabaseFromRDSProxy(dbProxy, port, tags)
		if err != nil {
			cfg.Log.Debugf("Could not convert RDS Proxy %q to database resource: %v.",
				aws.StringValue(dbProxy.DBProxyName), err)
		} else {
			databases = append(databases, database)
		}

		// Add custom endpoints.
		for _, customEndpoint := range customEndpointsByProxyName[aws.StringValue(dbProxy.DBProxyName)] {
			if !services.IsRDSProxyCustomEndpointAvailable(customEndpoint) {
				cfg.Log.Debugf("The current status of custom endpoint %q of RDS Proxy %q is %q. Skipping.",
					aws.StringValue(customEndpoint.DBProxyEndpointName),
					aws.StringValue(customEndpoint.DBProxyName),
					aws.StringValue(customEndpoint.Status))
				continue
			}

			database, err = services.NewDatabaseFromRDSProxyCustomEndpoint(dbProxy, customEndpoint, port, tags)
			if err != nil {
				cfg.Log.Debugf("Could not convert custom endpoint %q of RDS Proxy %q to database resource: %v.",
					aws.StringValue(customEndpoint.DBProxyEndpointName),
					aws.StringValue(customEndpoint.DBProxyName),
					err)
				continue
			}
			databases = append(databases, database)
		}
	}

	return databases, nil
}

// getRDSProxies fetches all RDS Proxies using the provided client, up to the
// specified max number of pages.
func getRDSProxies(ctx context.Context, rdsClient rdsiface.RDSAPI, maxPages int) (rdsProxies []*rds.DBProxy, err error) {
	var pageNum int
	err = rdsClient.DescribeDBProxiesPagesWithContext(
		ctx,
		&rds.DescribeDBProxiesInput{},
		func(ddo *rds.DescribeDBProxiesOutput, lastPage bool) bool {
			pageNum++
			rdsProxies = append(rdsProxies, ddo.DBProxies...)
			return pageNum <= maxPages
		},
	)
	return rdsProxies, trace.Wrap(libcloudaws.ConvertRequestFailureError(err))
}

// getRDSProxyCustomEndpoints fetches all RDS Proxy custom endpoints using the
// provided client.
func getRDSProxyCustomEndpoints(ctx context.Context, rdsClient rdsiface.RDSAPI, maxPages int) (map[string][]*rds.DBProxyEndpoint, error) {
	customEndpointsByProxyName := make(map[string][]*rds.DBProxyEndpoint)
	var pageNum int
	err := rdsClient.DescribeDBProxyEndpointsPagesWithContext(
		ctx,
		&rds.DescribeDBProxyEndpointsInput{},
		func(ddo *rds.DescribeDBProxyEndpointsOutput, lastPage bool) bool {
			pageNum++
			for _, customEndpoint := range ddo.DBProxyEndpoints {
				customEndpointsByProxyName[aws.StringValue(customEndpoint.DBProxyName)] = append(customEndpointsByProxyName[aws.StringValue(customEndpoint.DBProxyName)], customEndpoint)
			}
			return pageNum <= maxPages
		},
	)
	return customEndpointsByProxyName, trace.Wrap(libcloudaws.ConvertRequestFailureError(err))
}

// getRDSProxyTargetPort gets the port number that the targets of the RDS Proxy
// are using.
func getRDSProxyTargetPort(ctx context.Context, rdsClient rdsiface.RDSAPI, dbProxyName *string) (int64, error) {
	output, err := rdsClient.DescribeDBProxyTargetsWithContext(ctx, &rds.DescribeDBProxyTargetsInput{
		DBProxyName: dbProxyName,
	})
	if err != nil {
		return 0, trace.Wrap(libcloudaws.ConvertRequestFailureError(err))
	}

	// The proxy may have multiple targets but they should have the same port.
	for _, target := range output.Targets {
		if target.Port != nil {
			return aws.Int64Value(target.Port), nil
		}
	}
	return 0, trace.NotFound("RDS Proxy target port not found")
}

// listRDSResourceTags returns tags for provided RDS resource.
func listRDSResourceTags(ctx context.Context, rdsClient rdsiface.RDSAPI, resourceName *string) ([]*rds.Tag, error) {
	output, err := rdsClient.ListTagsForResourceWithContext(ctx, &rds.ListTagsForResourceInput{
		ResourceName: resourceName,
	})
	if err != nil {
		return nil, trace.Wrap(libcloudaws.ConvertRequestFailureError(err))
	}
	return output.TagList, nil
}
