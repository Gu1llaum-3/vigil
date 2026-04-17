//go:build linux

package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectRepositoriesDebian() ([]common.RepositoryInfo, error) {
	var repos []common.RepositoryInfo

	// Parse /etc/apt/sources.list
	if r, err := parseAptSourcesFile("/etc/apt/sources.list"); err == nil {
		repos = append(repos, r...)
	}

	// Parse /etc/apt/sources.list.d/*.list and *.sources
	for _, pattern := range []string{
		"/etc/apt/sources.list.d/*.list",
		"/etc/apt/sources.list.d/*.sources",
	} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, file := range files {
			if r, err := parseAptSourcesFile(file); err == nil {
				repos = append(repos, r...)
			}
		}
	}

	return repos, nil
}

func parseAptSourcesFile(path string) ([]common.RepositoryInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var repos []common.RepositoryInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle deb822 format (.sources files)
		if strings.HasSuffix(path, ".sources") {
			// Simplified: just note the file exists, skip full deb822 parsing
			continue
		}

		// Traditional one-liner: deb [options] url distribution components
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		enabled := true
		idx := 0
		repoType := fields[idx]
		if repoType == "deb-src" {
			// Only collect binary repos
			continue
		}
		if repoType != "deb" {
			continue
		}
		idx++

		// Skip [options]
		if strings.HasPrefix(fields[idx], "[") {
			idx++
		}
		if idx >= len(fields) {
			continue
		}

		url := fields[idx]
		idx++
		distribution := ""
		components := ""
		if idx < len(fields) {
			distribution = fields[idx]
			idx++
		}
		if idx < len(fields) {
			components = strings.Join(fields[idx:], " ")
		}

		name := repoNameFromURL(url)
		secure := strings.HasPrefix(url, "https://")

		repos = append(repos, common.RepositoryInfo{
			Name:         name,
			URL:          url,
			Enabled:      enabled,
			Secure:       secure,
			Distribution: distribution,
			Components:   components,
		})
	}
	return repos, nil
}

func repoNameFromURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}
