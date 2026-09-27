package docker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
)

type DigestSource interface {
	ImageDigests(context.Context, string) ([]string, error)
}

func (c *Client) ImageDigests(ctx context.Context, id string) ([]string, error) {
	var image struct{ RepoDigests []string }
	err := c.get(ctx, "/images/"+url.PathEscape(id)+"/json", &image)
	return image.RepoDigests, err
}

type updateResult struct {
	next      time.Time
	available *bool
}
type UpdateChecker struct {
	Local  DigestSource
	Client *http.Client
	Config config.ImageUpdates
	cache  map[string]updateResult
}

func NewUpdateChecker(local DigestSource, c config.ImageUpdates) *UpdateChecker {
	return &UpdateChecker{Local: local, Config: c, Client: &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cache: map[string]updateResult{}}
}
func manifestURL(ref string) (string, bool) {
	if strings.Contains(ref, "@") || strings.HasPrefix(ref, "sha256:") {
		return "", false
	}
	parts := strings.Split(ref, "/")
	host := "registry-1.docker.io"
	path := ref
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		host = parts[0]
		path = strings.Join(parts[1:], "/")
	}
	if host == "docker.io" || host == "index.docker.io" {
		host = "registry-1.docker.io"
	}
	if host == "registry-1.docker.io" && !strings.Contains(path, "/") {
		path = "library/" + path
	}
	tag := "latest"
	if i := strings.LastIndex(path, ":"); i > strings.LastIndex(path, "/") {
		tag = path[i+1:]
		path = path[:i]
	}
	if path == "" || tag == "" || strings.ContainsAny(path+tag, "?#") {
		return "", false
	}
	return "https://" + host + "/v2/" + path + "/manifests/" + url.PathEscape(tag), true
}
func (c *UpdateChecker) Check(ctx context.Context, ref, id string) (result *bool, err error) {
	key := ref + "@" + id
	if cached, ok := c.cache[key]; ok && time.Now().Before(cached.next) {
		return cached.available, nil
	}
	if len(c.cache) > 256 {
		c.cache = map[string]updateResult{}
	}
	defer func() { c.cache[key] = updateResult{next: time.Now().Add(c.Config.Interval), available: result} }()
	endpoint, ok := manifestURL(ref)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.Config.Timeout)
	defer cancel()
	local, err := c.Local.ImageDigests(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(local) == 0 {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anonymous registry unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, fmt.Errorf("registry manifest too large")
	}
	var manifest struct {
		Schema    int `json:"schemaVersion"`
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil || manifest.Schema != 2 {
		return nil, fmt.Errorf("unsupported registry manifest")
	}
	remote := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	matches := map[string]bool{remote: true}
	for _, m := range manifest.Manifests {
		matches[m.Digest] = true
	}
	available := true
	for _, digest := range local {
		_, hash, ok := strings.Cut(digest, "@")
		if ok && matches[hash] {
			available = false
		}
	}
	return &available, nil
}
