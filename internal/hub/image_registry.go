package hub

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type imageRegistryClient interface {
	HeadDigest(ctx context.Context, imageRef string) (string, error)
	ResolvedDigest(ctx context.Context, imageRef, architecture string) (string, error)
	ListTags(ctx context.Context, repository string) ([]string, error)
}

type remoteImageRegistryClient struct{}

func (remoteImageRegistryClient) HeadDigest(ctx context.Context, imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef, name.WeakValidation)
	if err != nil {
		return "", err
	}
	desc, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous))
	if err != nil {
		return "", err
	}
	return desc.Digest.String(), nil
}

func (remoteImageRegistryClient) ResolvedDigest(ctx context.Context, imageRef, architecture string) (string, error) {
	ref, err := name.ParseReference(imageRef, name.WeakValidation)
	if err != nil {
		return "", err
	}
	desc, err := remote.Get(
		ref,
		remote.WithContext(ctx),
		remote.WithAuth(authn.Anonymous),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: normalizePlatformArchitecture(architecture)}),
	)
	if err != nil {
		return "", err
	}
	return desc.Digest.String(), nil
}

func (remoteImageRegistryClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	repo, err := name.NewRepository(repository, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return remote.List(repo, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous))
}
