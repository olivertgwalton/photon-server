package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// partSize is what a put holds in memory at once. An object no larger is sent in one request; a
// larger one in parts of this size, of which S3 takes up to 10,000.
const partSize = 16 << 20

const (
	// statFor is how long what an object is is remembered, so a client is sent to it without the
	// bucket being asked each time; one removed by another node is sent to at most this long.
	statFor = 10 * time.Minute
	// statsKept is how many objects are remembered at once.
	statsKept = 50_000
	// linkLife is how long a link to an object works.
	linkLife = time.Hour
	// linkSlack is how long before a link stops working a client is told to stop following it, so
	// one followed late, or slowly, still works.
	linkSlack = 10 * time.Minute
)

// Config is where a bucket is and how it is reached.
type Config struct {
	// Endpoint is the store the bucket is in, as https://host[:port]; "" is Amazon S3.
	Endpoint string
	Bucket   string
	// Folder is where in the bucket objects are kept; "" is its root.
	Folder string
	// Region is the bucket's; "" asks the store.
	Region string
	// AccessKey and SecretKey sign each request. Without them AWS's own credentials do, from the
	// shared credentials file or the instance's role, as AWS's tools find them.
	AccessKey, SecretKey string
	// LinkEndpoint is where clients reach the store, as Endpoint is, to read the pictures, sounds
	// and videos they are sent to; "" sends them none, and their bytes go through the server.
	LinkEndpoint string
}

// Bucket keeps objects in an S3 bucket, under a prefix. An object is kept for good under its key,
// or replaced only by the same bytes.
type Bucket struct {
	client *minio.Client
	// links signs links at the endpoint clients reach the bucket at, or is nil where they are not
	// given links.
	links  *minio.Client
	stats  *stats
	bucket string
	// prefix is "" or a folder, ending in a slash.
	prefix string
}

// OpenBucket opens the bucket c names, once it answers that it is there.
func OpenBucket(ctx context.Context, c Config) (*Bucket, error) {
	endpoint, secure, err := parseEndpoint(c.Endpoint, "s3.amazonaws.com", true)
	if err != nil {
		return nil, err
	}
	creds := credentials.NewStaticV4(c.AccessKey, c.SecretKey, "")
	if c.AccessKey == "" && c.SecretKey == "" {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.FileAWSCredentials{}, &credentials.IAM{},
		})
	}
	// Addressed by name in the host for AWS and Google, and in the path anywhere else, as every
	// other store answers.
	region := c.Region
	client, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: secure, Region: region})
	if err != nil {
		return nil, err
	}
	b := (&Bucket{client: client, stats: &stats{kept: map[string]stat{}}, bucket: c.Bucket}).Within(c.Folder)
	ok, err := client.BucketExists(ctx, b.bucket)
	if err != nil {
		return nil, fmt.Errorf("the bucket %q did not answer: %w", b.bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("there is no bucket %q at %s", b.bucket, endpoint)
	}
	if c.LinkEndpoint != "" {
		public, publicSecure, err := parseEndpoint(c.LinkEndpoint, endpoint, secure)
		if err != nil {
			return nil, err
		}
		if region == "" {
			// Links are signed without asking the bucket, which clients reach where this server may not.
			if region, err = client.GetBucketLocation(ctx, b.bucket); err != nil {
				return nil, fmt.Errorf("the bucket %q did not say its region: %w", b.bucket, err)
			}
		}
		if b.links, err = minio.New(public, &minio.Options{Creds: creds, Secure: publicSecure, Region: region}); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// parseEndpoint answers the host of the address e, as http:// or https://host[:port], and whether
// it is reached over TLS; host and secure where e is "".
func parseEndpoint(e, host string, secure bool) (string, bool, error) {
	if e == "" {
		return host, secure, nil
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("the address %q is not http:// or https:// and a host", e)
	}
	return u.Host, u.Scheme == "https", nil
}

// Origin answers the origin of the links clients are sent to, or "" where they are sent none.
func (b *Bucket) Origin(ctx context.Context) (string, error) {
	if b.links == nil {
		return "", nil
	}
	u, err := b.links.PresignedGetObject(ctx, b.bucket, b.key("origin"), linkLife, nil)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host, nil
}

// Link answers a link that reads the object under key, as clients are sent to read it, or "" where
// they are sent none.
func (b *Bucket) Link(ctx context.Context, key string) (string, error) {
	if b.links == nil {
		return "", nil
	}
	u, err := b.links.PresignedGetObject(ctx, b.bucket, b.key(key), linkLife, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Within is the bucket under folder within its prefix.
func (b *Bucket) Within(folder string) *Bucket {
	prefix := b.prefix
	if folder = strings.Trim(folder, "/"); folder != "" {
		prefix += folder + "/"
	}
	return &Bucket{client: b.client, links: b.links, stats: b.stats, bucket: b.bucket, prefix: prefix}
}

func (b *Bucket) key(key string) string { return b.prefix + key }

// notFound is err as fs.ErrNotExist where it is the bucket saying there is no such object.
func notFound(key string, err error) error {
	if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
		return &fs.PathError{Op: "open", Path: key, Err: fs.ErrNotExist}
	}
	return err
}

// Open answers the object under key, or fs.ErrNotExist. Its bytes are read as they are asked for,
// from where they are sought.
func (b *Bucket) Open(ctx context.Context, key string) (Object, error) {
	if b.links != nil {
		return b.openLinked(ctx, key)
	}
	o, err := b.client.GetObject(ctx, b.bucket, b.key(key), minio.GetObjectOptions{})
	if err != nil {
		return Object{}, notFound(key, err)
	}
	info, err := o.Stat()
	if err != nil {
		return Object{}, errors.Join(notFound(key, err), o.Close())
	}
	return Object{ReadSeekCloser: o, ModTime: info.LastModified}, nil
}

// openLinked opens the object under key as one a client may be sent to read from the bucket, if
// it is a picture, a sound or a video as its bytes said when it was put: never a page or an SVG,
// which could run script.
func (b *Bucket) openLinked(ctx context.Context, key string) (Object, error) {
	info, ok := b.stats.get(b.key(key))
	if !ok {
		got, err := b.client.StatObject(ctx, b.bucket, b.key(key), minio.StatObjectOptions{})
		if err != nil {
			return Object{}, notFound(key, err)
		}
		info = stat{kind: got.ContentType, modTime: got.LastModified, at: time.Now()}
		b.stats.put(b.key(key), info)
	}
	// Nothing is asked of the bucket until the object is read, which a client sent to it never
	// makes it.
	o, err := b.client.GetObject(ctx, b.bucket, b.key(key), minio.GetObjectOptions{})
	if err != nil {
		return Object{}, err
	}
	kind, _, _ := strings.Cut(info.kind, "/")
	if kind != "image" && kind != "audio" && kind != "video" {
		return Object{ReadSeekCloser: o, ModTime: info.modTime}, nil
	}
	l := &linked{Object: o, links: b.links, bucket: b.bucket, key: b.key(key)}
	return Object{ReadSeekCloser: l, ModTime: info.modTime}, nil
}

// stats remembers what objects are: their type, as their bytes said, and when they were put.
type stats struct {
	mu   sync.Mutex
	kept map[string]stat
}

type stat struct {
	kind    string
	modTime time.Time
	// at is when it was asked of the bucket.
	at time.Time
}

func (s *stats) get(key string) (stat, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.kept[key]
	return st, ok && time.Since(st.at) < statFor
}

func (s *stats) put(key string, st stat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// ponytail: forgets everything when full, an LRU if a library's working set outgrows it.
	if len(s.kept) >= statsKept {
		clear(s.kept)
	}
	s.kept[key] = st
}

func (s *stats) forget(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.kept, key)
}

// Exists answers whether there is an object under key.
func (b *Bucket) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.client.StatObject(ctx, b.bucket, b.key(key), minio.StatObjectOptions{})
	if err := notFound(key, err); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Put keeps what r reads under key, in place of any object there. Should r fail, nothing is kept.
func (b *Bucket) Put(ctx context.Context, key string, r io.Reader) error {
	// minio-go sends an object of unknown size in parts, three requests at least, so what fits one
	// part is read first and sent whole.
	var head bytes.Buffer
	n, err := io.CopyN(&head, r, partSize)
	size := int64(-1)
	var body io.Reader
	switch {
	case errors.Is(err, io.EOF):
		size, body = n, &head
	case err != nil:
		return err
	default:
		body = io.MultiReader(&head, r)
	}
	b.stats.forget(b.key(key))
	// The type its bytes say, which a bucket answers a client sent to it with.
	kind := http.DetectContentType(head.Bytes())
	_, err = b.client.PutObject(ctx, b.bucket, b.key(key), body, size, minio.PutObjectOptions{PartSize: partSize, ContentType: kind})
	return err
}

// linked is an object a client may be sent to read from its bucket.
type linked struct {
	*minio.Object
	links  *minio.Client
	bucket string
	key    string
}

// link answers a link that reads the object, answered with header's Content-Type and
// Cache-Control where it has them, and the time to stop following it.
func (l *linked) link(ctx context.Context, header http.Header) (string, time.Time, error) {
	q := url.Values{}
	if v := header.Get("Content-Type"); v != "" {
		q.Set("response-content-type", v)
	}
	if v := header.Get("Cache-Control"); v != "" {
		q.Set("response-cache-control", v)
	}
	u, err := l.links.PresignedGetObject(ctx, l.bucket, l.key, linkLife, q)
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), time.Now().Add(linkLife - linkSlack), nil
}

// Delete removes the object under key, if there is one.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	b.stats.forget(b.key(key))
	return b.client.RemoveObject(ctx, b.bucket, b.key(key), minio.RemoveObjectOptions{})
}

// List answers the objects whose keys start with prefix, in the order of their keys.
func (b *Bucket) List(ctx context.Context, prefix string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		ctx, stop := context.WithCancel(ctx)
		defer stop()
		for o := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{Prefix: b.key(prefix), Recursive: true}) {
			if o.Err != nil {
				yield(Entry{}, o.Err)
				return
			}
			if !yield(Entry{Key: strings.TrimPrefix(o.Key, b.prefix), ModTime: o.LastModified}, nil) {
				return
			}
		}
	}
}
