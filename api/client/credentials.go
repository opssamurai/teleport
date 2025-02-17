/*
Copyright 2021 Gravitational, Inc.

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

package client

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"os"
	"sync"

	"github.com/gravitational/trace"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/http2"

	"github.com/gravitational/teleport/api/constants"
	"github.com/gravitational/teleport/api/defaults"
	"github.com/gravitational/teleport/api/identityfile"
	"github.com/gravitational/teleport/api/profile"
	"github.com/gravitational/teleport/api/utils"
	"github.com/gravitational/teleport/api/utils/keys"
	"github.com/gravitational/teleport/api/utils/sshutils"
)

// Credentials are used to authenticate the API auth client. Some Credentials
// also provide other functionality, such as automatic address discovery and
// ssh connectivity.
//
// See the examples below for an example of each loader.
type Credentials interface {
	// Dialer is used to create a dialer used to connect to the Auth server.
	Dialer(cfg Config) (ContextDialer, error)
	// TLSConfig returns TLS configuration used to authenticate the client.
	TLSConfig() (*tls.Config, error)
	// SSHClientConfig returns SSH configuration used to connect to the
	// Auth server through a reverse tunnel.
	SSHClientConfig() (*ssh.ClientConfig, error)
}

// LoadTLS is used to load Credentials directly from a *tls.Config.
//
// TLS creds can only be used to connect directly to a Teleport Auth server.
func LoadTLS(tlsConfig *tls.Config) Credentials {
	return &tlsConfigCreds{
		tlsConfig: tlsConfig,
	}
}

// tlsConfigCreds use a defined *tls.Config to provide client credentials.
type tlsConfigCreds struct {
	tlsConfig *tls.Config
}

// Dialer is used to dial a connection to an Auth server.
func (c *tlsConfigCreds) Dialer(cfg Config) (ContextDialer, error) {
	return nil, trace.NotImplemented("no dialer")
}

// TLSConfig returns TLS configuration.
func (c *tlsConfigCreds) TLSConfig() (*tls.Config, error) {
	if c.tlsConfig == nil {
		return nil, trace.BadParameter("tls config is nil")
	}
	return configureTLS(c.tlsConfig), nil
}

// SSHClientConfig returns SSH configuration.
func (c *tlsConfigCreds) SSHClientConfig() (*ssh.ClientConfig, error) {
	return nil, trace.NotImplemented("no ssh config")
}

// LoadKeyPair is used to load Credentials from a certicate keypair on disk.
//
// KeyPair Credentials can only be used to connect directly to a Teleport Auth server.
//
// New KeyPair files can be generated with tsh or tctl.
//
//	$ tctl auth sign --format=tls --user=api-user --out=path/to/certs
//
// The certificates' time to live can be specified with --ttl.
//
// See the example below for usage.
func LoadKeyPair(certFile, keyFile, caFile string) Credentials {
	return &keypairCreds{
		certFile: certFile,
		keyFile:  keyFile,
		caFile:   caFile,
	}
}

// keypairCreds use keypair certificates to provide client credentials.
type keypairCreds struct {
	certFile string
	keyFile  string
	caFile   string
}

// Dialer is used to dial a connection to an Auth server.
func (c *keypairCreds) Dialer(cfg Config) (ContextDialer, error) {
	return nil, trace.NotImplemented("no dialer")
}

// TLSConfig returns TLS configuration.
func (c *keypairCreds) TLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	cas, err := os.ReadFile(c.caFile)
	if err != nil {
		return nil, trace.ConvertSystemError(err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(cas); !ok {
		return nil, trace.BadParameter("invalid TLS CA cert PEM")
	}

	return configureTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}), nil
}

// SSHClientConfig returns SSH configuration.
func (c *keypairCreds) SSHClientConfig() (*ssh.ClientConfig, error) {
	return nil, trace.NotImplemented("no ssh config")
}

// LoadIdentityFile is used to load Credentials from an identity file on disk.
//
// Identity Credentials can be used to connect to an auth server directly
// or through a reverse tunnel.
//
// A new identity file can be generated with tsh or tctl.
//
//	$ tsh login --user=api-user --out=identity-file-path
//	$ tctl auth sign --user=api-user --out=identity-file-path
//
// The identity file's time to live can be specified with --ttl.
//
// See the example below for usage.
func LoadIdentityFile(path string) Credentials {
	return &identityCredsFile{
		path: path,
	}
}

// identityCredsFile use an identity file to provide client credentials.
type identityCredsFile struct {
	identityFile *identityfile.IdentityFile
	path         string
}

// Dialer is used to dial a connection to an Auth server.
func (c *identityCredsFile) Dialer(cfg Config) (ContextDialer, error) {
	return nil, trace.NotImplemented("no dialer")
}

// TLSConfig returns TLS configuration.
func (c *identityCredsFile) TLSConfig() (*tls.Config, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	tlsConfig, err := c.identityFile.TLSConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return configureTLS(tlsConfig), nil
}

// SSHClientConfig returns SSH configuration.
func (c *identityCredsFile) SSHClientConfig() (*ssh.ClientConfig, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	sshConfig, err := c.identityFile.SSHClientConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return sshConfig, nil
}

// load is used to lazy load the identity file from persistent storage.
// This allows LoadIdentity to avoid possible errors for UX purposes.
func (c *identityCredsFile) load() error {
	if c.identityFile != nil {
		return nil
	}
	var err error
	if c.identityFile, err = identityfile.ReadFile(c.path); err != nil {
		return trace.BadParameter("identity file could not be decoded: %v", err)
	}
	return nil
}

// LoadIdentityFileFromString is used to load Credentials from a string containing identity file contents.
//
// Identity Credentials can be used to connect to an auth server directly
// or through a reverse tunnel.
//
// A new identity file can be generated with tsh or tctl.
//
//	$ tsh login --user=api-user --out=identity-file-path
//	$ tctl auth sign --user=api-user --out=identity-file-path
//
// The identity file's time to live can be specified with --ttl.
//
// See the example below for usage.
func LoadIdentityFileFromString(content string) Credentials {
	return &identityCredsString{
		content: content,
	}
}

// identityCredsString use an identity file loaded to string to provide client credentials.
type identityCredsString struct {
	identityFile *identityfile.IdentityFile
	content      string
}

// Dialer is used to dial a connection to an Auth server.
func (c *identityCredsString) Dialer(cfg Config) (ContextDialer, error) {
	return nil, trace.NotImplemented("no dialer")
}

// TLSConfig returns TLS configuration.
func (c *identityCredsString) TLSConfig() (*tls.Config, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	tlsConfig, err := c.identityFile.TLSConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return configureTLS(tlsConfig), nil
}

// SSHClientConfig returns SSH configuration.
func (c *identityCredsString) SSHClientConfig() (*ssh.ClientConfig, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	sshConfig, err := c.identityFile.SSHClientConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return sshConfig, nil
}

// load is used to lazy load the identity file from a string.
func (c *identityCredsString) load() error {
	if c.identityFile != nil {
		return nil
	}
	var err error
	if c.identityFile, err = identityfile.FromString(c.content); err != nil {
		return trace.BadParameter("identity file could not be decoded: %v", err)
	}
	return nil
}

// LoadProfile is used to load Credentials from a tsh profile on disk.
//
// dir is the profile directory. It will defaults to "~/.tsh".
//
// name is the profile name. It will default to the currently active tsh profile.
//
// Profile Credentials can be used to connect to an auth server directly
// or through a reverse tunnel.
//
// Profile Credentials will automatically attempt to find your reverse
// tunnel address and make a connection through it.
//
// A new profile can be generated with tsh.
//
//	$ tsh login --user=api-user
func LoadProfile(dir, name string) Credentials {
	return &profileCreds{
		dir:  dir,
		name: name,
	}
}

// profileCreds use a tsh profile to provide client credentials.
type profileCreds struct {
	dir     string
	name    string
	profile *profile.Profile
}

// Dialer is used to dial a connection to an Auth server.
func (c *profileCreds) Dialer(cfg Config) (ContextDialer, error) {
	sshConfig, err := c.SSHClientConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	tlsConfig, err := c.profile.TLSConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return NewProxyDialer(
		*sshConfig,
		cfg.KeepAlivePeriod,
		cfg.DialTimeout,
		c.profile.WebProxyAddr,
		cfg.InsecureAddressDiscovery,
		WithTLSConfig(tlsConfig),
	), nil
}

// TLSConfig returns TLS configuration.
func (c *profileCreds) TLSConfig() (*tls.Config, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	tlsConfig, err := c.profile.TLSConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return configureTLS(tlsConfig), nil
}

// SSHClientConfig returns SSH configuration.
func (c *profileCreds) SSHClientConfig() (*ssh.ClientConfig, error) {
	if err := c.load(); err != nil {
		return nil, trace.Wrap(err)
	}

	sshConfig, err := c.profile.SSHClientConfig()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return sshConfig, nil
}

// load is used to lazy load the profile from persistent storage.
// This allows LoadProfile to avoid possible errors for UX purposes.
func (c *profileCreds) load() error {
	if c.profile != nil {
		return nil
	}
	var err error
	if c.profile, err = profile.FromDir(c.dir, c.name); err != nil {
		return trace.BadParameter("profile could not be decoded: %v", err)
	}
	return nil
}

func configureTLS(c *tls.Config) *tls.Config {
	tlsConfig := c.Clone()
	tlsConfig.NextProtos = utils.Deduplicate(append(tlsConfig.NextProtos, http2.NextProtoTLS))

	// If SNI isn't set, set it to the default name that can be found
	// on all Teleport issued certificates. This is needed because we
	// don't always know which host we will be connecting to.
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = constants.APIDomain
	}

	// This logic still appears to be necessary to force client to always send
	// a certificate regardless of the server setting. Otherwise the client may pick
	// not to send the client certificate by looking at certificate request.
	if len(tlsConfig.Certificates) > 0 {
		cert := tlsConfig.Certificates[0]
		tlsConfig.Certificates = nil
		tlsConfig.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &cert, nil
		}
	}

	return tlsConfig
}

// DynamicIdentityFileCreds allows a changing identity file to be used as the
// source of authentication for Client. It does not automatically watch the
// identity file or reload on an interval, this is left as an exercise for the
// consumer.
type DynamicIdentityFileCreds struct {
	// mu protects the fields that may change if the underlying identity file
	// is reloaded.
	mu            sync.RWMutex
	tlsCert       *tls.Certificate
	tlsRootCAs    *x509.CertPool
	sshCert       *ssh.Certificate
	sshKey        crypto.Signer
	sshKnownHosts []ssh.PublicKey

	// Path is the path to the identity file to load and reload.
	Path string
}

// NewDynamicIdentityFileCreds returns a DynamicIdentityFileCreds which has
// been initially loaded and is ready for use.
func NewDynamicIdentityFileCreds(path string) (*DynamicIdentityFileCreds, error) {
	d := &DynamicIdentityFileCreds{
		Path: path,
	}
	if err := d.Reload(); err != nil {
		return nil, trace.Wrap(err)
	}
	return d, nil
}

// Reload causes the identity file to be re-read from the disk. It will return
// an error if loading the credentials fails.
func (d *DynamicIdentityFileCreds) Reload() error {
	id, err := identityfile.ReadFile(d.Path)
	if err != nil {
		return trace.Wrap(err)
	}

	// This section is essentially id.TLSConfig()
	cert, err := keys.X509KeyPair(id.Certs.TLS, id.PrivateKey)
	if err != nil {
		return trace.Wrap(err)
	}
	pool := x509.NewCertPool()
	for _, caCerts := range id.CACerts.TLS {
		if !pool.AppendCertsFromPEM(caCerts) {
			return trace.BadParameter("invalid CA cert PEM")
		}
	}

	// This sections is essentially id.SSHClientConfig()
	sshCert, err := sshutils.ParseCertificate(id.Certs.SSH)
	if err != nil {
		return trace.Wrap(err)
	}
	sshPrivateKey, err := keys.ParsePrivateKey(id.PrivateKey)
	if err != nil {
		return trace.Wrap(err)
	}
	knownHosts, err := sshutils.ParseKnownHosts(id.CACerts.SSH)
	if err != nil {
		return trace.Wrap(err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.tlsRootCAs = pool
	d.tlsCert = &cert
	d.sshCert = sshCert
	d.sshKey = sshPrivateKey
	d.sshKnownHosts = knownHosts
	return nil
}

// Dialer returns a dialer for the client to use. This is not used, but is
// needed to implement the Credentials interface.
func (d *DynamicIdentityFileCreds) Dialer(
	_ Config,
) (ContextDialer, error) {
	// Returning a dialer isn't necessary for this credential.
	return nil, trace.NotImplemented("no dialer")
}

// TLSConfig returns TLS configuration. Implementing the Credentials interface.
func (d *DynamicIdentityFileCreds) TLSConfig() (*tls.Config, error) {
	// Build a "dynamic" tls.Config which can support a changing cert and root
	// CA pool.
	cfg := &tls.Config{
		// Set the default NextProto of "h2". Based on the value in
		// configureTLS()
		NextProtos: []string{http2.NextProtoTLS},

		// GetClientCertificate is used instead of the static Certificates
		// field.
		Certificates: nil,
		GetClientCertificate: func(
			_ *tls.CertificateRequestInfo,
		) (*tls.Certificate, error) {
			// GetClientCertificate callback is used to allow us to dynamically
			// change the certificate when reloaded.
			d.mu.RLock()
			defer d.mu.RUnlock()
			return d.tlsCert, nil
		},

		// VerifyConnection is used instead of the static RootCAs field.
		RootCAs: nil,
		// InsecureSkipVerify is forced true to ensure that only our
		// VerifyConnection callback is used to verify the server's presented
		// certificate.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			// This VerifyConnection callback is based on the standard library
			// implementation of verifyServerCertificate in the `tls` package.
			// We provide our own implementation so we can dynamically handle
			// a changing CA Roots pool.
			d.mu.RLock()
			defer d.mu.RUnlock()
			opts := x509.VerifyOptions{
				DNSName:       state.ServerName,
				Intermediates: x509.NewCertPool(),
				Roots:         d.tlsRootCAs,
			}
			for _, cert := range state.PeerCertificates[1:] {
				// Whilst we don't currently use intermediate certs at
				// Teleport, including this here means that we are
				// future-proofed in case we do.
				opts.Intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(opts)
			return err
		},
		// Set ServerName for SNI & Certificate Validation to the sentinel
		// teleport.cluster.local which is included on all Teleport Auth Server
		// certificates. Based on the value in configureTLS()
		ServerName: constants.APIDomain,
	}

	return cfg, nil
}

// SSHClientConfig returns SSH configuration, implementing the Credentials
// interface.
func (d *DynamicIdentityFileCreds) SSHClientConfig() (*ssh.ClientConfig, error) {
	hostKeyCallback, err := sshutils.NewHostKeyCallback(sshutils.HostKeyCallbackConfig{
		GetHostCheckers: func() ([]ssh.PublicKey, error) {
			d.mu.RLock()
			defer d.mu.RUnlock()
			return d.sshKnownHosts, nil
		},
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Build a "dynamic" ssh config. Based roughly on
	// `sshutils.ProxyClientSSHConfig` with modifications to make it work with
	// dynamically changing credentials and CAs.
	cfg := &ssh.ClientConfig{
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(func() (signers []ssh.Signer, err error) {
				d.mu.RLock()
				defer d.mu.RUnlock()
				sshSigner, err := sshutils.SSHSigner(d.sshCert, d.sshKey)
				if err != nil {
					return nil, trace.Wrap(err)
				}
				return []ssh.Signer{sshSigner}, nil
			}),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         defaults.DefaultIOTimeout,
		// We use this because we can't always guarantee that a user will have
		// a principal other than this (they may not have access to SSH nodes)
		// and the actual user here doesn't matter for auth server API
		// authentication. All that matters is that the principal specified here
		// is stable across all certificates issued to the user, since this
		// value cannot be changed in a following rotation -
		// SSHSessionJoinPrincipal is included on all user ssh certs.
		//
		// This is a bit of a hack - the ideal solution is a refactor of the
		// API client in order to support the SSH config being generated at
		// time of use, rather than a single SSH config being made dynamic.
		// ~ noah
		User: "-teleport-internal-join",
	}
	return cfg, nil
}
