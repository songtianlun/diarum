package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/songtianlun/diarum/internal/imaging"
)

// ErrPosterTooLarge is returned for a video poster over the size cap.
var ErrPosterTooLarge = errors.New("video poster is too large")

// PosterMedia returns a copy of media that points at its video poster, so
// the regular open/save/delete helpers work on it; nil when it has none.
func PosterMedia(media *Media) *Media {
	if media == nil || media.Poster == "" {
		return nil
	}
	poster := *media
	poster.File = media.Poster
	return &poster
}

// SaveMediaPoster stores reader as the poster frame of a video, beside it
// (the video must be saved first), and records it on the media.
func (s *Store) SaveMediaPoster(media *Media, reader io.Reader) error {
	if media == nil {
		return fmt.Errorf("media is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, imaging.MaxPosterBytes+1))
	if err != nil {
		return err
	}
	if len(data) > imaging.MaxPosterBytes {
		return ErrPosterTooLarge
	}
	poster := *media
	poster.File = imaging.PosterFilename(media.File)
	if err := s.saveMediaCompanion(media, &poster, bytes.NewReader(data)); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE media SET poster = ?, updated = ? WHERE id = ?`, poster.File, nowString(), media.ID); err != nil {
		_ = s.deleteMediaObject(&poster)
		return err
	}
	media.Poster = poster.File
	s.forgetPublicURLs(media)
	return nil
}

// OpenMediaSeeker opens a stored media file for random access, which serving
// video with range requests needs. A file on disk is opened as is; one in
// object storage is read in ranges, on demand, so a large video is never
// loaded whole. os.ErrNotExist when the file is absent.
func (s *Store) OpenMediaSeeker(media *Media) (io.ReadSeekCloser, error) {
	for _, candidate := range s.mediaFileCandidates(media) {
		if !fileExists(candidate) {
			continue
		}
		if file, err := os.Open(candidate); err == nil {
			return file, nil
		}
	}
	if media != nil && media.Owner != "" {
		if cfg := s.userS3Config(media.Owner); cfg != nil {
			client, err := NewS3Client(cfg)
			if err != nil {
				return nil, err
			}
			seeker, err := s.openS3Seeker(client, cfg, media)
			if err == nil {
				return seeker, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	if s.legacyS3Client == nil || s.LegacyS3 == nil {
		return nil, os.ErrNotExist
	}
	return s.openS3Seeker(s.legacyS3Client, s.LegacyS3, media)
}

// openS3Seeker finds the object of media (HEAD) and returns a reader over it.
func (s *Store) openS3Seeker(client *awss3.Client, cfg *LegacyS3Config, media *Media) (io.ReadSeekCloser, error) {
	if client == nil || cfg == nil {
		return nil, os.ErrNotExist
	}
	for _, key := range s.mediaObjectKeys(media, cfg) {
		head, err := client.HeadObject(context.Background(), &awss3.HeadObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)})
		if err != nil {
			continue
		}
		return &s3Seeker{client: client, bucket: cfg.Bucket, key: key, size: aws.ToInt64(head.ContentLength)}, nil
	}
	return nil, os.ErrNotExist
}

// s3Seeker reads an S3 object as an io.ReadSeeker: each read after a seek
// starts one ranged GET from the new offset and keeps streaming from it.
type s3Seeker struct {
	client *awss3.Client
	bucket string
	key    string
	size   int64
	offset int64
	body   io.ReadCloser
}

func (r *s3Seeker) Read(p []byte) (int, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	// A stream that ends early is asked for again from where it stopped;
	// one that gives nothing on a fresh request is broken.
	for fresh := r.body == nil; ; fresh = true {
		if r.body == nil {
			out, err := r.client.GetObject(context.Background(), &awss3.GetObjectInput{
				Bucket: aws.String(r.bucket),
				Key:    aws.String(r.key),
				Range:  aws.String(fmt.Sprintf("bytes=%d-", r.offset)),
			})
			if err != nil {
				return 0, err
			}
			r.body = out.Body
		}
		n, err := r.body.Read(p)
		r.offset += int64(n)
		if err != io.EOF || r.offset >= r.size {
			return n, err
		}
		r.closeBody()
		if n > 0 {
			return n, nil
		}
		if fresh {
			return 0, io.ErrUnexpectedEOF
		}
	}
}

func (r *s3Seeker) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.offset + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, errors.New("s3 seeker: invalid whence")
	}
	if next < 0 {
		return 0, errors.New("s3 seeker: negative position")
	}
	if next != r.offset {
		r.closeBody()
		r.offset = next
	}
	return next, nil
}

func (r *s3Seeker) closeBody() {
	if r.body != nil {
		_ = r.body.Close()
		r.body = nil
	}
}

func (r *s3Seeker) Close() error {
	r.closeBody()
	return nil
}
