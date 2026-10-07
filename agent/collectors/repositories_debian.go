//go:build linux

package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectRepositoriesDebian() ([]common.RepositoryInfo, error) {
	return collectAptRepositories("/etc/apt")
}

// collectAptRepositories reads the one-line sources.list format (sources.list and
// sources.list.d/*.list) and the deb822 format (sources.list.d/*.sources, the default on
// Debian 13 and Ubuntu 24.04) under aptDir.
func collectAptRepositories(aptDir string) ([]common.RepositoryInfo, error) {
	var repos []common.RepositoryInfo

	if r, err := parseAptSourcesFile(filepath.Join(aptDir, "sources.list")); err == nil {
		repos = append(repos, r...)
	}

	for _, source := range []struct {
		pattern string
		parse   func(string) ([]common.RepositoryInfo, error)
	}{
		{"*.list", parseAptSourcesFile},
		{"*.sources", parseDeb822SourcesFile},
	} {
		files, err := filepath.Glob(filepath.Join(aptDir, "sources.list.d", source.pattern))
		if err != nil {
			continue
		}
		for _, file := range files {
			if r, err := source.parse(file); err == nil {
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
	defer func() { _ = f.Close() }() // read-only

	var repos []common.RepositoryInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
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

// parseDeb822SourcesFile parses a deb822-style .sources file (see sources.list(5)): stanzas
// separated by blank lines, case-insensitive "Field: value" lines, and continuation lines
// starting with whitespace (e.g. an inline Signed-By key). Each binary ("deb") stanza yields
// one entry per URI x suite. "Enabled: no" stanzas are kept and reported as disabled.
func parseDeb822SourcesFile(path string) ([]common.RepositoryInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only

	var repos []common.RepositoryInfo
	stanza := map[string]string{}
	lastField := ""
	flush := func() {
		repos = append(repos, deb822StanzaRepositories(stanza)...)
		stanza = map[string]string{}
		lastField = ""
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
			// Comment lines may appear anywhere, including inside a stanza.
		case raw[0] == ' ' || raw[0] == '\t':
			// Continuation of the previous field; none of the fields we read span lines.
			if lastField != "" {
				stanza[lastField] += " " + line
			}
		default:
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			lastField = strings.ToLower(strings.TrimSpace(name))
			stanza[lastField] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return repos, nil
}

func deb822StanzaRepositories(stanza map[string]string) []common.RepositoryInfo {
	if !slices.Contains(strings.Fields(stanza["types"]), "deb") {
		return nil
	}
	enabled := true
	switch strings.ToLower(stanza["enabled"]) {
	case "no", "false", "0":
		enabled = false
	}
	components := strings.Join(strings.Fields(stanza["components"]), " ")

	var repos []common.RepositoryInfo
	for _, url := range strings.Fields(stanza["uris"]) {
		for _, suite := range strings.Fields(stanza["suites"]) {
			repos = append(repos, common.RepositoryInfo{
				Name:         repoNameFromURL(url),
				URL:          url,
				Enabled:      enabled,
				Secure:       strings.HasPrefix(url, "https://"),
				Distribution: suite,
				Components:   components,
			})
		}
	}
	return repos
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
