package ghupdate

import (
	"errors"
	"strings"
)

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadUrl string `json:"browser_download_url"`
	Id          int    `json:"id"`
	Size        int    `json:"size"`
}

type release struct {
	Name      string          `json:"name"`
	Tag       string          `json:"tag_name"`
	Published string          `json:"published_at"`
	Url       string          `json:"html_url"`
	Body      string          `json:"body"`
	Assets    []*releaseAsset `json:"assets"`
	Id        int             `json:"id"`
}

// findChecksumsAsset returns the release's checksums file (goreleaser publishes a
// "*checksums.txt"). Returns an error if none is present so the updater can fail closed.
func (r *release) findChecksumsAsset() (*releaseAsset, error) {
	for _, asset := range r.Assets {
		n := strings.ToLower(asset.Name)
		if strings.Contains(n, "checksum") && strings.HasSuffix(n, ".txt") {
			return asset, nil
		}
	}
	return nil, errors.New("release has no checksums file; refusing to update")
}

// findAssetBySuffix returns the first available asset containing the specified suffix.
func (r *release) findAssetBySuffix(suffix string) (*releaseAsset, error) {
	if suffix != "" {
		for _, asset := range r.Assets {
			if strings.HasSuffix(asset.Name, suffix) {
				return asset, nil
			}
		}
	}

	return nil, errors.New("missing asset containing " + suffix)
}
