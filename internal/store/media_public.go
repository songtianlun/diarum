package store

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// publicKeyMissTTL is how long an object found missing is not looked up
// again, e.g. a variant still being generated.
const publicKeyMissTTL = time.Minute

type publicKeyEntry struct {
	key     string
	checked time.Time
}

// NormalizeS3PublicURL checks the public base URL of an S3 bucket, e.g. a CDN
// or custom domain serving the bucket root: "" (none) or an absolute http(s)
// URL without query or fragment. A trailing slash is dropped.
func NormalizeS3PublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("the public URL must start with http:// or https:// and include a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", errors.New("the public URL must not contain a query, fragment or credentials")
	}
	if len(raw) > 500 {
		return "", errors.New("the public URL is longer than 500 characters")
	}
	return strings.TrimRight(raw, "/"), nil
}

// MediaPublicURL returns the address media can be downloaded from directly,
// bypassing Diarum, when its owner stores images on S3 and set a public URL
// for the bucket. The object is looked up once (HEAD) so a missing file, such
// as a variant not generated yet, is never linked to; ok is false then, and
// the caller serves the file itself.
func (s *Store) MediaPublicURL(media *Media) (string, bool) {
	if media == nil || media.Storage != MediaStorageS3 {
		return "", false
	}
	cfg := s.userS3Config(media.Owner)
	if cfg == nil || cfg.PublicURL == "" {
		return "", false
	}
	cacheKey := media.ID + "/" + media.File
	if cached, ok := s.publicKeys.Load(cacheKey); ok {
		entry := cached.(publicKeyEntry)
		if entry.key != "" {
			return joinPublicURL(cfg.PublicURL, entry.key), true
		}
		if time.Since(entry.checked) < publicKeyMissTTL {
			return "", false
		}
	}
	client, err := NewS3Client(cfg)
	if err != nil || client == nil {
		return "", false
	}
	found := ""
	for _, key := range s.mediaObjectKeys(media, cfg) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)})
		cancel()
		if err == nil {
			found = key
			break
		}
	}
	s.publicKeys.Store(cacheKey, publicKeyEntry{key: found, checked: time.Now()})
	if found == "" {
		return "", false
	}
	return joinPublicURL(cfg.PublicURL, found), true
}

// forgetPublicURLs drops what MediaPublicURL remembered about media and its
// variants, once their files are gone.
func (s *Store) forgetPublicURLs(media *Media) {
	prefix := media.ID + "/"
	s.publicKeys.Range(func(key, _ any) bool {
		if strings.HasPrefix(key.(string), prefix) {
			s.publicKeys.Delete(key)
		}
		return true
	})
}

func joinPublicURL(base, key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return base + "/" + strings.Join(segments, "/")
}
