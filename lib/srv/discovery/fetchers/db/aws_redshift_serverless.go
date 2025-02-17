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
	"github.com/aws/aws-sdk-go/service/redshiftserverless"
	"github.com/aws/aws-sdk-go/service/redshiftserverless/redshiftserverlessiface"
	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/cloud"
	libcloudaws "github.com/gravitational/teleport/lib/cloud/aws"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/srv/discovery/common"
)

// newRedshiftServerlessFetcher returns a new AWS fetcher for Redshift
// Serverless databases.
func newRedshiftServerlessFetcher(cfg awsFetcherConfig) (common.Fetcher, error) {
	return newAWSFetcher(cfg, &redshiftServerlessPlugin{})
}

type workgroupWithTags struct {
	*redshiftserverless.Workgroup

	Tags []*redshiftserverless.Tag
}

// redshiftServerlessPlugin retrieves Redshift Serverless databases.
type redshiftServerlessPlugin struct{}

func (f *redshiftServerlessPlugin) ComponentShortName() string {
	// (r)ed(s)hift (s)erver(<)less
	return "rss<"
}

// rssAPI is a type alias for brevity alone.
type rssAPI = redshiftserverlessiface.RedshiftServerlessAPI

// GetDatabases returns Redshift Serverless databases matching the watcher's selectors.
func (f *redshiftServerlessPlugin) GetDatabases(ctx context.Context, cfg *awsFetcherConfig) (types.Databases, error) {
	client, err := cfg.AWSClients.GetAWSRedshiftServerlessClient(ctx, cfg.Region,
		cloud.WithAssumeRole(cfg.AssumeRole.RoleARN, cfg.AssumeRole.ExternalID),
		cloud.WithCredentialsMaybeIntegration(cfg.Integration),
	)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	databases, workgroups, err := getDatabasesFromWorkgroups(ctx, client, cfg.Log)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if len(workgroups) > 0 {
		vpcEndpointDatabases, err := getDatabasesFromVPCEndpoints(ctx, workgroups, client, cfg.Log)
		if err != nil {
			if trace.IsAccessDenied(err) {
				cfg.Log.Debugf("No permission to get Redshift Serverless VPC endpoints: %v.", err)
			} else {
				cfg.Log.Warnf("Failed to get Redshift Serverless VPC endpoints: %v.", err)
			}
		}

		databases = append(databases, vpcEndpointDatabases...)
	}
	return databases, nil
}

func getDatabasesFromWorkgroups(ctx context.Context, client rssAPI, log logrus.FieldLogger) (types.Databases, []*workgroupWithTags, error) {
	workgroups, err := getRSSWorkgroups(ctx, client)
	if err != nil {
		return nil, nil, trace.Wrap(err)
	}

	var databases types.Databases
	var workgroupsWithTags []*workgroupWithTags
	for _, workgroup := range workgroups {
		if !services.IsAWSResourceAvailable(workgroup, workgroup.Status) {
			log.Debugf("The current status of Redshift Serverless workgroup %v is %v. Skipping.", aws.StringValue(workgroup.WorkgroupName), aws.StringValue(workgroup.Status))
			continue
		}

		tags := getRSSResourceTags(ctx, workgroup.WorkgroupArn, client, log)
		database, err := services.NewDatabaseFromRedshiftServerlessWorkgroup(workgroup, tags)
		if err != nil {
			log.WithError(err).Infof("Could not convert Redshift Serverless workgroup %q to database resource.", aws.StringValue(workgroup.WorkgroupName))
			continue
		}

		databases = append(databases, database)
		workgroupsWithTags = append(workgroupsWithTags, &workgroupWithTags{
			Workgroup: workgroup,
			Tags:      tags,
		})
	}
	return databases, workgroupsWithTags, nil
}

func getDatabasesFromVPCEndpoints(ctx context.Context, workgroups []*workgroupWithTags, client rssAPI, log logrus.FieldLogger) (types.Databases, error) {
	endpoints, err := getRSSVPCEndpoints(ctx, client)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var databases types.Databases
	for _, endpoint := range endpoints {
		workgroup, found := findWorkgroupWithName(workgroups, aws.StringValue(endpoint.WorkgroupName))
		if !found {
			log.Debugf("Could not find matching workgroup for Redshift Serverless endpoint %v. Skipping.", aws.StringValue(endpoint.EndpointName))
			continue
		}

		if !services.IsAWSResourceAvailable(endpoint, endpoint.EndpointStatus) {
			log.Debugf("The current status of Redshift Serverless endpoint %v is %v. Skipping.", aws.StringValue(endpoint.EndpointName), aws.StringValue(endpoint.EndpointStatus))
			continue
		}

		// VPC endpoints do not have resource tags attached to them. Use the
		// tags from the workgroups instead.
		database, err := services.NewDatabaseFromRedshiftServerlessVPCEndpoint(endpoint, workgroup.Workgroup, workgroup.Tags)
		if err != nil {
			log.WithError(err).Infof("Could not convert Redshift Serverless endpoint %q to database resource.", aws.StringValue(endpoint.EndpointName))
			continue
		}
		databases = append(databases, database)
	}
	return databases, nil
}

func getRSSResourceTags(ctx context.Context, arn *string, client rssAPI, log logrus.FieldLogger) []*redshiftserverless.Tag {
	output, err := client.ListTagsForResourceWithContext(ctx, &redshiftserverless.ListTagsForResourceInput{
		ResourceArn: arn,
	})
	if err != nil {
		// Log errors here and return nil.
		if trace.IsAccessDenied(err) {
			log.WithError(err).Debugf("No Permission to get tags for %q.", aws.StringValue(arn))
		} else {
			log.WithError(err).Warnf("Failed to get tags for %q.", aws.StringValue(arn))
		}
		return nil
	}
	return output.Tags
}

func getRSSWorkgroups(ctx context.Context, client rssAPI) ([]*redshiftserverless.Workgroup, error) {
	var pages [][]*redshiftserverless.Workgroup
	err := client.ListWorkgroupsPagesWithContext(ctx, nil, func(page *redshiftserverless.ListWorkgroupsOutput, lastPage bool) bool {
		pages = append(pages, page.Workgroups)
		return len(pages) <= maxAWSPages
	})
	return flatten(pages), libcloudaws.ConvertRequestFailureError(err)
}

func getRSSVPCEndpoints(ctx context.Context, client rssAPI) ([]*redshiftserverless.EndpointAccess, error) {
	var pages [][]*redshiftserverless.EndpointAccess
	err := client.ListEndpointAccessPagesWithContext(ctx, nil, func(page *redshiftserverless.ListEndpointAccessOutput, lastPage bool) bool {
		pages = append(pages, page.Endpoints)
		return len(pages) <= maxAWSPages
	})
	return flatten(pages), libcloudaws.ConvertRequestFailureError(err)
}

func findWorkgroupWithName(workgroups []*workgroupWithTags, name string) (*workgroupWithTags, bool) {
	for _, workgroup := range workgroups {
		if aws.StringValue(workgroup.WorkgroupName) == name {
			return workgroup, true
		}
	}
	return nil, false
}
