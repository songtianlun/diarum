package backup

import (
	"context"
	"errors"
	"io"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awstypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/songtianlun/diarum/internal/store"
)

// ErrObjectNotFound reports a missing object.
var ErrObjectNotFound = errors.New("object not found")

// Object is an entry of a listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// ObjectStore is the part of S3 backups need.
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
}

// Archives are uploaded in parts of this size, so a failed part is retried on
// its own and archives can grow far beyond the 5 GB single-request limit.
const uploadPartSize = 16 << 20

type s3Store struct {
	client   *awss3.Client
	uploader *manager.Uploader
	bucket   string
}

// NewS3Store connects to an S3-compatible destination. It uses the same
// client setup as S3 image uploads.
func NewS3Store(cfg S3Config) (ObjectStore, error) {
	client, err := store.NewS3Client(&store.LegacyS3Config{
		Enabled:        true,
		Bucket:         cfg.Bucket,
		Region:         cfg.Region,
		Endpoint:       cfg.Endpoint,
		AccessKey:      cfg.AccessKey,
		Secret:         cfg.Secret,
		ForcePathStyle: cfg.ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}
	return newS3Store(client, cfg.Bucket), nil
}

func newS3Store(client *awss3.Client, bucket string) *s3Store {
	return &s3Store{
		client: client,
		bucket: bucket,
		uploader: manager.NewUploader(client, func(u *manager.Uploader) {
			u.PartSize = uploadPartSize
			u.Concurrency = 3
		}),
	}
}

func (s *s3Store) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.uploader.Upload(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return err
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}
	return out.Body, nil
}

func (s *s3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	objects := make([]Object, 0)
	pages := awss3.NewListObjectsV2Paginator(s.client, &awss3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			objects = append(objects, Object{Key: aws.ToString(obj.Key), Size: aws.ToInt64(obj.Size), LastModified: aws.ToTime(obj.LastModified)})
		}
	}
	return objects, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func isNotFound(err error) bool {
	var noSuchKey *awstypes.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	return false
}
