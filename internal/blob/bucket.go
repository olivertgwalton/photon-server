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

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// partSize is what a put holds in memory at once. An object no larger is sent in one request; a
// larger one in parts of this size, of which S3 takes up to 10,000.
const partSize = 16 << 20

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
	// environment, the shared credentials file, or the instance's role, as AWS's tools find them.
	AccessKey, SecretKey string
}

// Bucket keeps objects in an S3 bucket, under a prefix.
type Bucket struct {
	client *minio.Client
	bucket string
	// prefix is "" or a folder, ending in a slash.
	prefix string
}

// OpenBucket opens the bucket c names, once it answers that it is there.
func OpenBucket(ctx context.Context, c Config) (*Bucket, error) {
	endpoint, secure := "s3.amazonaws.com", true
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("the address %q is not http:// or https:// and a host", c.Endpoint)
		}
		endpoint, secure = u.Host, u.Scheme == "https"
	}
	creds := credentials.NewStaticV4(c.AccessKey, c.SecretKey, "")
	if c.AccessKey == "" && c.SecretKey == "" {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{}, &credentials.FileAWSCredentials{}, &credentials.IAM{},
		})
	}
	// Addressed by name in the host for AWS and Google, and in the path anywhere else, as every
	// other store answers.
	client, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: secure, Region: c.Region})
	if err != nil {
		return nil, err
	}
	b := (&Bucket{client: client, bucket: c.Bucket}).Within(c.Folder)
	ok, err := client.BucketExists(ctx, b.bucket)
	if err != nil {
		return nil, fmt.Errorf("the bucket %q did not answer: %w", b.bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("there is no bucket %q at %s", b.bucket, endpoint)
	}
	return b, nil
}

// Within is the bucket under folder within its prefix.
func (b *Bucket) Within(folder string) *Bucket {
	prefix := b.prefix
	if folder = strings.Trim(folder, "/"); folder != "" {
		prefix += folder + "/"
	}
	return &Bucket{client: b.client, bucket: b.bucket, prefix: prefix}
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
	o, err := b.client.GetObject(ctx, b.bucket, b.key(key), minio.GetObjectOptions{})
	if err != nil {
		return Object{}, notFound(key, err)
	}
	info, err := o.Stat()
	if err != nil {
		_ = o.Close()
		return Object{}, notFound(key, err)
	}
	return Object{ReadSeekCloser: o, ModTime: info.LastModified}, nil
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
	_, err = b.client.PutObject(ctx, b.bucket, b.key(key), body, size, minio.PutObjectOptions{PartSize: partSize})
	return err
}

// Delete removes the object under key, if there is one.
func (b *Bucket) Delete(ctx context.Context, key string) error {
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
