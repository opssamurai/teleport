/*
Copyright 2023 Gravitational, Inc.

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

package awsoidc

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/gravitational/trace"
	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport/api/types"
)

type mockIntegrationsTokenGenerator struct {
	proxies      []types.Server
	integrations map[string]types.Integration
}

// GetIntegration returns the specified integration resources.
func (m *mockIntegrationsTokenGenerator) GetIntegration(ctx context.Context, name string) (types.Integration, error) {
	if ig, found := m.integrations[name]; found {
		return ig, nil
	}

	return nil, trace.NotFound("integration not found")
}

// GetProxies returns a list of registered proxies.
func (m *mockIntegrationsTokenGenerator) GetProxies() ([]types.Server, error) {
	return m.proxies, nil
}

// GenerateAWSOIDCToken generates a token to be used to execute an AWS OIDC Integration action.
func (m *mockIntegrationsTokenGenerator) GenerateAWSOIDCToken(ctx context.Context, req types.GenerateAWSOIDCTokenRequest) (string, error) {
	return "token-goes-here", nil
}

func TestNewSessionV1(t *testing.T) {
	ctx := context.Background()

	dummyIntegration, err := types.NewIntegrationAWSOIDC(
		types.Metadata{Name: "myawsintegration"},
		&types.AWSOIDCIntegrationSpecV1{
			RoleARN: "arn:aws:sts::123456789012:role/TestRole",
		},
	)
	require.NoError(t, err)

	dummyProxy, err := types.NewServer(
		"proxy-123", types.KindProxy,
		types.ServerSpecV2{
			PublicAddrs: []string{"https://localhost:3080/"},
		},
	)
	require.NoError(t, err)

	for _, tt := range []struct {
		name             string
		region           string
		integration      string
		expectedErr      require.ErrorAssertionFunc
		sessionValidator func(*testing.T, *session.Session)
	}{
		{
			name:        "valid",
			region:      "us-dummy-1",
			integration: "myawsintegration",
			expectedErr: require.NoError,
			sessionValidator: func(t *testing.T, s *session.Session) {
				require.Equal(t, aws.String("us-dummy-1"), s.Config.Region)
			},
		},
		{
			name:        "not found error when integration is missing",
			region:      "us-dummy-1",
			integration: "not-found",
			expectedErr: notFounCheck,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockTokenGenertor := &mockIntegrationsTokenGenerator{
				proxies: []types.Server{dummyProxy},
				integrations: map[string]types.Integration{
					dummyIntegration.GetName(): dummyIntegration,
				},
			}
			awsSessionOut, err := NewSessionV1(ctx, mockTokenGenertor, tt.region, tt.integration)

			tt.expectedErr(t, err)
			if tt.sessionValidator != nil {
				tt.sessionValidator(t, awsSessionOut)
			}
		})
	}

}
