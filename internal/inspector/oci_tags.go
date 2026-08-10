package inspector

import (
	"context"
	"net/http"

	"helm.sh/helm/v4/pkg/helmpath"
	"helm.sh/helm/v4/pkg/registry"

	ociregistry "oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// ociClient wraps Helm's registry client to list raw OCI tags instead of
// Helm's built-in Tags(), which silently drops any tag that isn't strict
// semver (e.g. tags prefixed with "v", such as ghcr.io/kserve/charts
// publishes). Pull is delegated to Helm unchanged.
type ociClient struct {
	*registry.Client
}

func (ociClient) Tags(ref string) ([]string, error) {
	parsedRef, err := ociregistry.ParseReference(ref)
	if err != nil {
		return nil, err
	}
	repository, err := remote.NewRepository(parsedRef.String())
	if err != nil {
		return nil, err
	}
	authorizer, err := ociAuthorizer()
	if err != nil {
		return nil, err
	}
	repository.Client = authorizer
	return collectOCITags(context.Background(), repository)
}

// collectOCITags aggregates every page of the repository's raw tag list,
// unlike Helm's Client.Tags() it applies no semver filtering or normalization
// so callers see exactly what the registry reports.
func collectOCITags(ctx context.Context, repository *remote.Repository) ([]string, error) {
	var tags []string
	err := repository.Tags(ctx, "", func(page []string) error {
		tags = append(tags, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// ociAuthorizer mirrors the credential resolution registry.NewClient performs
// internally (Helm credentials file with Docker config fallback), since that
// state isn't exposed on *registry.Client for reuse here.
func ociAuthorizer() (*auth.Client, error) {
	storeOptions := credentials.StoreOptions{
		AllowPlaintextPut:        true,
		DetectDefaultNativeStore: true,
	}
	store, err := credentials.NewStore(helmpath.ConfigPath(registry.CredentialsFileBasename), storeOptions)
	if err != nil {
		return nil, err
	}
	credentialsStore := credentials.Store(store)
	if dockerStore, err := credentials.NewStoreFromDocker(storeOptions); err == nil {
		credentialsStore = credentials.NewStoreWithFallbacks(store, dockerStore)
	}

	authorizer := &auth.Client{Client: &http.Client{Transport: registry.NewTransport(false)}}
	authorizer.SetUserAgent("chart-release-inspector")
	authorizer.Credential = credentials.Credential(credentialsStore)
	return authorizer, nil
}
