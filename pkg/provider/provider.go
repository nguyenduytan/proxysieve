// Package provider defines small optional capabilities, not vendor conditionals.
package provider

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

type Capabilities struct {
	FetchEndpoints   bool `json:"fetch_endpoints"`
	BuildCredentials bool `json:"build_credentials"`
	ReadQuota        bool `json:"read_quota"`
	RotateSession    bool `json:"rotate_session"`
}
type Config struct {
	SourceID      model.ID
	CredentialRef secret.Ref
	Options       map[string]string // Non-secret provider options only.
}
type Provider interface {
	ID() model.ID
	DisplayName() string
	Capabilities() Capabilities
	ValidateConfig(context.Context, Config) error
}
type EndpointFetcher interface {
	FetchEndpoints(context.Context, Config) ([]proxy.Endpoint, error)
}
type CredentialRequest struct{ Country, Region, City, Session string }
type Credentials struct{ Username, Password secret.Value }
type CredentialBuilder interface {
	BuildCredential(context.Context, Config, CredentialRequest) (Credentials, error)
}
