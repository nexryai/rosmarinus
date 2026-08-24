package instancemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/nexryai/rosmarinus/internal/domain/instances"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
)

const maxMetadataResponseSize = 1024 * 1024

type Fetcher struct {
	client   *http.Client
	validate func(*url.URL) error
}

func New(timeout time.Duration, userAgent string, allowedNetworks []string, client *http.Client) *Fetcher {
	if client == nil {
		client, validate := mediafetch.NewSafeHTTPClient(timeout, userAgent, allowedNetworks)
		return &Fetcher{client: client, validate: validate}
	}
	clone := *client
	if clone.Timeout == 0 {
		clone.Timeout = timeout
	}
	return &Fetcher{client: &clone, validate: mediafetch.ValidateURL}
}

func (f *Fetcher) Fetch(ctx context.Context, host string) (instances.Metadata, error) {
	base, err := url.Parse("https://" + strings.TrimSpace(host))
	if err != nil || base.Hostname() == "" {
		return instances.Metadata{}, fmt.Errorf("invalid instance host")
	}
	if err := f.validate(base); err != nil {
		return instances.Metadata{}, err
	}
	metadata := instances.Metadata{}
	nodeInfoErr := f.fetchNodeInfo(ctx, base, &metadata)
	document, htmlErr := f.fetchHTML(ctx, base)
	if htmlErr == nil {
		applyHTMLMetadata(document, &metadata)
	}
	manifestURL := document.ManifestURL
	if manifestURL == "" {
		manifestURL = base.ResolveReference(&url.URL{Path: "/manifest.json"}).String()
	}
	if err := f.fetchManifest(ctx, manifestURL, &metadata); err != nil && document.ManifestURL != "" {
		_ = f.fetchManifest(ctx, base.ResolveReference(&url.URL{Path: "/manifest.json"}).String(), &metadata)
	}
	if metadata.FaviconURL == "" {
		metadata.FaviconURL = f.fetchDefaultFavicon(ctx, base)
	}
	if nodeInfoErr != nil && htmlErr != nil {
		return instances.Metadata{}, fmt.Errorf("fetch instance metadata: nodeinfo: %v; html: %v", nodeInfoErr, htmlErr)
	}
	return metadata, nil
}

func (f *Fetcher) fetchNodeInfo(ctx context.Context, base *url.URL, metadata *instances.Metadata) error {
	var wellKnown struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := f.getJSON(ctx, base.ResolveReference(&url.URL{Path: "/.well-known/nodeinfo"}), &wellKnown); err != nil {
		return err
	}
	var selected string
	for _, rel := range []string{
		"http://nodeinfo.diaspora.software/ns/schema/2.1",
		"http://nodeinfo.diaspora.software/ns/schema/2.0",
		"http://nodeinfo.diaspora.software/ns/schema/1.0",
	} {
		for _, link := range wellKnown.Links {
			if link.Rel == rel {
				selected = link.Href
				break
			}
		}
		if selected != "" {
			break
		}
	}
	if selected == "" {
		return fmt.Errorf("nodeinfo link is missing")
	}
	target, err := url.Parse(selected)
	if err != nil || target.Hostname() == "" {
		return fmt.Errorf("nodeinfo link is invalid")
	}
	var info struct {
		Software struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"software"`
		OpenRegistrations *bool `json:"openRegistrations"`
		Usage             struct {
			Users struct {
				Total int64 `json:"total"`
			} `json:"users"`
			LocalPosts    int64 `json:"localPosts"`
			LocalComments int64 `json:"localComments"`
		} `json:"usage"`
		Metadata struct {
			Name            string `json:"name"`
			NodeName        string `json:"nodeName"`
			Description     string `json:"description"`
			NodeDescription string `json:"nodeDescription"`
			ThemeColor      string `json:"themeColor"`
			Maintainer      struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"maintainer"`
		} `json:"metadata"`
	}
	if err := f.getJSON(ctx, target, &info); err != nil {
		return err
	}
	metadata.NodeInfoFetched = true
	metadata.SoftwareName = bounded(info.Software.Name, 64)
	metadata.SoftwareVersion = bounded(info.Software.Version, 64)
	metadata.OpenRegistrations = info.OpenRegistrations
	metadata.UsersCount = maxInt64(info.Usage.Users.Total, 0)
	metadata.NotesCount = maxInt64(info.Usage.LocalPosts, 0) + maxInt64(info.Usage.LocalComments, 0)
	metadata.Name = bounded(firstNonEmpty(info.Metadata.NodeName, info.Metadata.Name), 256)
	metadata.Description = bounded(firstNonEmpty(info.Metadata.NodeDescription, info.Metadata.Description), 4096)
	metadata.ThemeColor = bounded(info.Metadata.ThemeColor, 64)
	metadata.MaintainerName = bounded(info.Metadata.Maintainer.Name, 128)
	metadata.MaintainerEmail = bounded(info.Metadata.Maintainer.Email, 256)
	return nil
}

type htmlMetadata struct {
	Name, Description, ThemeColor    string
	IconURL, FaviconURL, ManifestURL string
}

func (f *Fetcher) fetchHTML(ctx context.Context, base *url.URL) (htmlMetadata, error) {
	res, err := f.get(ctx, base)
	if err != nil {
		return htmlMetadata{}, err
	}
	body, err := readBoundedBody(res)
	if err != nil {
		return htmlMetadata{}, err
	}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return htmlMetadata{}, err
	}
	result := htmlMetadata{}
	walkHTML(document, base, &result)
	return result, nil
}

func walkHTML(node *html.Node, base *url.URL, metadata *htmlMetadata) {
	if node.Type == html.ElementNode {
		attrs := htmlAttrs(node)
		switch strings.ToLower(node.Data) {
		case "meta":
			key := strings.ToLower(firstNonEmpty(attrs["name"], attrs["property"]))
			value := strings.TrimSpace(attrs["content"])
			switch key {
			case "application-name", "og:site_name":
				if metadata.Name == "" {
					metadata.Name = value
				}
			case "og:title":
				if metadata.Name == "" {
					metadata.Name = value
				}
			case "description", "og:description":
				if metadata.Description == "" {
					metadata.Description = value
				}
			case "theme-color":
				if metadata.ThemeColor == "" {
					metadata.ThemeColor = value
				}
			}
		case "link":
			rels := strings.Fields(strings.ToLower(attrs["rel"]))
			href := resolveMetadataURL(base, attrs["href"])
			switch {
			case contains(rels, "manifest") && metadata.ManifestURL == "":
				metadata.ManifestURL = href
			case (contains(rels, "apple-touch-icon-precomposed") || contains(rels, "apple-touch-icon")) && metadata.IconURL == "":
				metadata.IconURL = href
			case contains(rels, "icon") && metadata.FaviconURL == "":
				metadata.FaviconURL = href
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, base, metadata)
	}
}

func applyHTMLMetadata(document htmlMetadata, metadata *instances.Metadata) {
	if metadata.Name == "" {
		metadata.Name = bounded(document.Name, 256)
	}
	if metadata.Description == "" {
		metadata.Description = bounded(document.Description, 4096)
	}
	if metadata.ThemeColor == "" {
		metadata.ThemeColor = bounded(document.ThemeColor, 64)
	}
	metadata.IconURL = bounded(document.IconURL, 512)
	metadata.FaviconURL = bounded(document.FaviconURL, 512)
}

func (f *Fetcher) fetchManifest(ctx context.Context, rawURL string, metadata *instances.Metadata) error {
	target, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	var manifest struct {
		Name        string `json:"name"`
		ShortName   string `json:"short_name"`
		Description string `json:"description"`
		ThemeColor  string `json:"theme_color"`
		Icons       []struct {
			Src string `json:"src"`
		} `json:"icons"`
	}
	if err := f.getJSON(ctx, target, &manifest); err != nil {
		return err
	}
	if metadata.Name == "" {
		metadata.Name = bounded(firstNonEmpty(manifest.Name, manifest.ShortName), 256)
	}
	if metadata.Description == "" {
		metadata.Description = bounded(manifest.Description, 4096)
	}
	if metadata.ThemeColor == "" {
		metadata.ThemeColor = bounded(manifest.ThemeColor, 64)
	}
	if metadata.IconURL == "" && len(manifest.Icons) > 0 {
		metadata.IconURL = bounded(resolveMetadataURL(target, manifest.Icons[0].Src), 512)
	}
	return nil
}

func (f *Fetcher) fetchDefaultFavicon(ctx context.Context, base *url.URL) string {
	target := base.ResolveReference(&url.URL{Path: "/favicon.ico"})
	if err := f.validate(target); err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		return ""
	}
	res, err := f.client.Do(req)
	if err != nil {
		return ""
	}
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ""
	}
	return target.String()
}

func (f *Fetcher) getJSON(ctx context.Context, target *url.URL, destination any) error {
	res, err := f.get(ctx, target)
	if err != nil {
		return err
	}
	body, err := readBoundedBody(res)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return fmt.Errorf("decode metadata JSON: %w", err)
	}
	return nil
}

func (f *Fetcher) get(ctx context.Context, target *url.URL) (*http.Response, error) {
	if err := f.validate(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("metadata status %d", res.StatusCode)
	}
	return res, nil
}

func readBoundedBody(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	if res.ContentLength > maxMetadataResponseSize {
		return nil, fmt.Errorf("metadata response exceeds %d bytes", maxMetadataResponseSize)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxMetadataResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxMetadataResponseSize {
		return nil, fmt.Errorf("metadata response exceeds %d bytes", maxMetadataResponseSize)
	}
	return body, nil
}

func htmlAttrs(node *html.Node) map[string]string {
	result := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		result[strings.ToLower(attr.Key)] = attr.Val
	}
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func resolveMetadataURL(base *url.URL, raw string) string {
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return ""
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "https" || resolved.Hostname() == "" || resolved.User != nil {
		return ""
	}
	return resolved.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func bounded(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}
