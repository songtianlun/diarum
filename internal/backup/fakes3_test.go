package backup

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeS3 is a small path-style S3 server: enough of the API for the real SDK
// client (single and multipart uploads, get, list, delete) to talk to it.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	uploads map[string]map[int][]byte
	failAll int // respond with this status to everything when non-zero
	server  *httptest.Server
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	f := &fakeS3{objects: make(map[string][]byte), uploads: make(map[string]map[int][]byte)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeS3) config() S3Config {
	return S3Config{Bucket: "bucket", Region: "us-east-1", Endpoint: f.server.URL, AccessKey: "key", Secret: "secret", ForcePathStyle: true, Prefix: "backups"}
}

func writeXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+body)
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll != 0 {
		writeXML(w, f.failAll, `<Error><Code>InternalError</Code><Message>boom</Message></Error>`)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != "bucket" {
		writeXML(w, http.StatusNotFound, `<Error><Code>NoSuchBucket</Code><Message>no bucket</Message></Error>`)
		return
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && key == "":
		prefix := q.Get("prefix")
		keys := make([]string, 0)
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			var escaped strings.Builder
			_ = xml.EscapeText(&escaped, []byte(k))
			fmt.Fprintf(&sb, `<Contents><Key>%s</Key><Size>%d</Size><LastModified>2024-01-01T00:00:00.000Z</LastModified></Contents>`, escaped.String(), len(f.objects[k]))
		}
		writeXML(w, http.StatusOK, fmt.Sprintf(`<ListBucketResult><Name>bucket</Name><KeyCount>%d</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>%s</ListBucketResult>`, len(keys), sb.String()))
	case r.Method == http.MethodPost && q.Has("uploads"):
		id := fmt.Sprintf("upload-%d", len(f.uploads)+1)
		f.uploads[id] = make(map[int][]byte)
		writeXML(w, http.StatusOK, fmt.Sprintf(`<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, id))
	case r.Method == http.MethodPut && q.Has("uploadId"):
		var part int
		fmt.Sscan(q.Get("partNumber"), &part)
		body, _ := io.ReadAll(r.Body)
		f.uploads[q.Get("uploadId")][part] = body
		w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, part))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		parts := f.uploads[q.Get("uploadId")]
		numbers := make([]int, 0, len(parts))
		for n := range parts {
			numbers = append(numbers, n)
		}
		sort.Ints(numbers)
		var data []byte
		for _, n := range numbers {
			data = append(data, parts[n]...)
		}
		f.objects[key] = data
		delete(f.uploads, q.Get("uploadId"))
		writeXML(w, http.StatusOK, fmt.Sprintf(`<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>%s</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`, key))
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		delete(f.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[key] = body
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			writeXML(w, http.StatusNotFound, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeXML(w, http.StatusMethodNotAllowed, `<Error><Code>MethodNotAllowed</Code><Message>no</Message></Error>`)
	}
}
