// Copyright 2022 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"context"
	"net"
	"time"

	"github.com/gravitational/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	devicepb "github.com/gravitational/teleport/api/gen/proto/go/teleport/devicetrust/v1"
	"github.com/gravitational/teleport/api/utils/grpc/interceptors"
	"github.com/gravitational/teleport/lib/devicetrust/native"
)

// Opt is a creation option for [E]
type Opt func(*E)

// WithAutoCreateDevice instructs EnrollDevice to automatically create the
// requested device, if it wasn't previously registered.
// See also [FakeEnrollmentToken].
func WithAutoCreateDevice(b bool) Opt {
	return func(e *E) {
		e.Service.autoCreateDevice = b
	}
}

// E is an integrated test environment for device trust.
type E struct {
	DevicesClient devicepb.DeviceTrustServiceClient
	Service       *FakeDeviceService

	closers []func() error
}

// Close tears down the test environment.
func (e *E) Close() error {
	var errs []error
	for i := len(e.closers) - 1; i >= 0; i-- {
		if err := e.closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	return trace.NewAggregate(errs...)
}

// MustNew creates a new E or panics.
// Callers are required to defer e.Close() to release test resources.
func MustNew(opts ...Opt) *E {
	env, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return env
}

// New creates a new E.
// Callers are required to defer e.Close() to release test resources.
func New(opts ...Opt) (*E, error) {
	e := &E{
		Service: newFakeDeviceService(),
	}

	for _, opt := range opts {
		opt(e)
	}

	ok := false
	defer func() {
		if !ok {
			e.Close()
		}
	}()

	// gRPC Server.
	const bufSize = 100 // arbitrary
	lis := bufconn.Listen(bufSize)
	e.closers = append(e.closers, lis.Close)

	s := grpc.NewServer(
		// Options below are similar to auth.GRPCServer.
		grpc.StreamInterceptor(interceptors.GRPCServerStreamErrorInterceptor),
		grpc.UnaryInterceptor(interceptors.GRPCServerUnaryErrorInterceptor),
	)
	e.closers = append(e.closers, func() error {
		s.GracefulStop()
		s.Stop()
		return nil
	})

	// Register service.
	devicepb.RegisterDeviceTrustServiceServer(s, e.Service)

	// Start.
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()

	// gRPC client.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := grpc.DialContext(ctx, "unused",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStreamInterceptor(interceptors.GRPCClientStreamErrorInterceptor),
		grpc.WithUnaryInterceptor(interceptors.GRPCClientUnaryErrorInterceptor),
	)
	if err != nil {
		return nil, err
	}
	e.closers = append(e.closers, cc.Close)
	e.DevicesClient = devicepb.NewDeviceTrustServiceClient(cc)

	ok = true
	return e, nil
}

// FakeDevice is implemented by the platform-native fakes and is used in tests
// for device authentication and enrollment.
type FakeDevice interface {
	CollectDeviceData(mode native.CollectDataMode) (*devicepb.DeviceCollectedData, error)
	EnrollDeviceInit() (*devicepb.EnrollDeviceInit, error)
	GetDeviceOSType() devicepb.OSType
	SignChallenge(chal []byte) (sig []byte, err error)
	SolveTPMEnrollChallenge(challenge *devicepb.TPMEnrollChallenge, debug bool) (*devicepb.TPMEnrollChallengeResponse, error)
	SolveTPMAuthnDeviceChallenge(challenge *devicepb.TPMAuthenticateDeviceChallenge) (*devicepb.TPMAuthenticateDeviceChallengeResponse, error)
	GetDeviceCredential() *devicepb.DeviceCredential
}
