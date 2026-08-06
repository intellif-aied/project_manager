package workspaceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const resolverTimeout = 2 * time.Second

var repositoryCache sync.Map

// RepositoryKey returns an irreversible identity for the Git repository that
// contains cwd. Credentials and the remote URL never leave the client.
func RepositoryKey(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" || !filepath.IsAbs(cwd) {
		return ""
	}
	cleaned := filepath.Clean(cwd)
	if cached, ok := repositoryCache.Load(cleaned); ok {
		return cached.(string)
	}
	ctx, cancel := context.WithTimeout(context.Background(), resolverTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "-C", cleaned, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		repositoryCache.Store(cleaned, "")
		return ""
	}
	key := KeyFromRemote(string(output))
	repositoryCache.Store(cleaned, key)
	return key
}

func KeyFromRemote(remote string) string {
	normalized := normalizeRemote(remote)
	if normalized == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("git-repository/v1\x00" + normalized))
	return hex.EncodeToString(digest[:])
}

func normalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if !strings.Contains(remote, "://") {
		if at := strings.LastIndex(remote, "@"); at >= 0 {
			remote = remote[at+1:]
		}
		if colon := strings.Index(remote, ":"); colon > 0 {
			host := strings.ToLower(strings.TrimSpace(remote[:colon]))
			return cleanRemoteIdentity(host, remote[colon+1:])
		}
		return cleanRemoteIdentity("local", filepath.ToSlash(filepath.Clean(remote)))
	}
	parsed, err := url.Parse(remote)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" && parsed.Scheme == "file" {
		host = "local"
	}
	return cleanRemoteIdentity(host, parsed.Path)
}

func cleanRemoteIdentity(host, repoPath string) string {
	host = strings.TrimSpace(host)
	repoPath = strings.TrimSpace(strings.ReplaceAll(repoPath, `\`, "/"))
	repoPath = strings.TrimPrefix(path.Clean("/"+repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	repoPath = strings.Trim(repoPath, "/")
	if host == "" || repoPath == "" || repoPath == "." {
		return ""
	}
	return host + "/" + repoPath
}
