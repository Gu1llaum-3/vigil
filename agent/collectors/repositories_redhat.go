//go:build linux

package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectRepositoriesRedHat() ([]common.RepositoryInfo, error) {
	files, err := filepath.Glob("/etc/yum.repos.d/*.repo")
	if err != nil {
		return nil, err
	}

	var repos []common.RepositoryInfo
	for _, file := range files {
		r, err := parseRepoFile(file)
		if err != nil {
			continue
		}
		repos = append(repos, r...)
	}
	return repos, nil
}

func parseRepoFile(path string) ([]common.RepositoryInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var repos []common.RepositoryInfo
	current := map[string]string{}
	var section string

	flushSection := func() {
		if section == "" {
			return
		}
		url := current["baseurl"]
		if url == "" {
			url = current["mirrorlist"]
		}
		enabled := current["enabled"] != "0"
		secure := strings.HasPrefix(url, "https://")
		repos = append(repos, common.RepositoryInfo{
			Name:    current["name"],
			URL:     url,
			Enabled: enabled,
			Secure:  secure,
		})
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flushSection()
			section = line[1 : len(line)-1]
			current = map[string]string{}
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			current[key] = val
		}
	}
	flushSection()
	return repos, nil
}
