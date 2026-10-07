//go:build integration

package blob

import (
	"context"
	"os"
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
// test.
func newBucket(t *testing.T) *Bucket {
	t.Helper()
	c := testBucket(t)
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

func TestABucketKeepsObjectsWholeOrNotAtAll(t *testing.T) { testWholeOrNotAtAll(t, newBucket(t)) }

func TestABucketListsObjectsByPrefix(t *testing.T) { testListedByPrefix(t, newBucket(t)) }

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
