//go:build integration

package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"uuid"
)

// testBucket is the bucket TEST_S3_ENDPOINT and TEST_S3_BUCKET name, signed for with AWS's own
// credentials from the environment.
func testBucket(t *testing.T) Config {
	t.Helper()
	c := Config{Endpoint: os.Getenv("TEST_S3_ENDPOINT"), Bucket: os.Getenv("TEST_S3_BUCKET")}
	if c.Endpoint == "" || c.Bucket == "" {
		t.Fatal("TEST_S3_ENDPOINT and TEST_S3_BUCKET are unset")
	}
	return c
}

// newBucket is a fresh folder of the test bucket, signed for with keys given, emptied after the
// test; with links, it sends clients to read what it keeps.
func newBucket(t *testing.T, links bool) *Bucket {
	t.Helper()
	c := testBucket(t)
	if links {
		c.LinkEndpoint = c.Endpoint
	}
	c.Folder = uuid.NewV7().String()
	c.AccessKey, c.SecretKey = os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	b, err := OpenBucket(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for e, err := range b.List(ctx, "") {
			if err == nil {
				_ = b.Delete(ctx, e.Key)
			}
		}
	})
	return b
}

func TestABucketKeepsObjectsWholeOrNotAtAll(t *testing.T) {
	testWholeOrNotAtAll(t, newBucket(t, false))
}

func TestABucketListsObjectsByPrefix(t *testing.T) { testListedByPrefix(t, newBucket(t, false)) }

func TestABucketIsOpenedWithTheKeysGivenOrAWSsOwn(t *testing.T) {
	if _, err := OpenBucket(t.Context(), testBucket(t)); err != nil {
		t.Errorf("opened with AWS's own credentials: %v", err)
	}
	wrong := testBucket(t)
	wrong.AccessKey, wrong.SecretKey = os.Getenv("AWS_ACCESS_KEY_ID"), "wrong"
	if _, err := OpenBucket(t.Context(), wrong); err == nil {
		t.Error("a bucket was opened with the wrong secret key")
	}
}

func TestABucketThatIsNotThereIsRefused(t *testing.T) {
	c := testBucket(t)
	c.Bucket = "missing-" + uuid.NewV7().String()
	if _, err := OpenBucket(t.Context(), c); err == nil {
		t.Error("a bucket that is not there was opened")
	}
}

func TestAPictureIsSentToItsBucketWhereClientsAreSent(t *testing.T) {
	b := newBucket(t, true)
	ctx := t.Context()
	jpeg := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 1000)...)
	if err := b.Put(ctx, "poster", bytes.NewReader(jpeg)); err != nil {
		t.Fatal(err)
	}
	o, err := b.Open(ctx, "poster")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	header := http.Header{"Content-Type": {"image/jpeg"}, "Cache-Control": {"private, max-age=31536000, immutable"}}
	if err := Serve(rec, httptest.NewRequest(http.MethodGet, "/poster", nil), o, "", header); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("answered %d, want a redirect to the bucket", rec.Code)
	}
	var age int
	if _, err := fmt.Sscanf(rec.Header().Get("Cache-Control"), "private, max-age=%d", &age); err != nil || age <= 0 || age > int((linkLife-linkSlack).Seconds()) {
		t.Errorf("redirect kept for %q, want less than the link lasts", rec.Header().Get("Cache-Control"))
	}
	origin, err := b.Origin(ctx)
	if err != nil || !strings.HasPrefix(rec.Header().Get("Location"), origin+"/") {
		t.Errorf("sent to %q, want somewhere at %q (%v)", rec.Header().Get("Location"), origin, err)
	}
	resp, err := http.Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, jpeg) {
		t.Errorf("the link answered %s and %d bytes, want the poster's %d", resp.Status, len(got), len(jpeg))
	}
	if ct, cc := resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"); ct != "image/jpeg" || cc != header.Get("Cache-Control") {
		t.Errorf("the link answered %q, kept as %q; want the server's own headers", ct, cc)
	}
	// What the bucket was asked of the poster is remembered, but never past its removal.
	if err := b.Delete(ctx, "poster"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(ctx, "poster"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening a removed poster: %v, want none", err)
	}
}

func TestADocumentIsNeverSentToItsBucket(t *testing.T) {
	b := newBucket(t, true)
	ctx := t.Context()
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	if err := b.Put(ctx, "logo", strings.NewReader(svg)); err != nil {
		t.Fatal(err)
	}
	o, err := b.Open(ctx, "logo")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := Serve(rec, httptest.NewRequest(http.MethodGet, "/logo", nil), o, "logo.svg", nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != svg {
		t.Errorf("answered %d with %q, want the SVG served here, under this server's policy", rec.Code, rec.Body.String())
	}
}
