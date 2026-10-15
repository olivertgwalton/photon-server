package animelist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// maxList bounds each list read; both are under 10 MB today.
	maxList = 64 << 20
	// minEntries is the fewest entries a download must hold to replace the lists kept: fewer is a
	// list cut short or broken. The lists hold about 10,000.
	minEntries = 5000
)

// ErrShort is a list read whole that holds too few entries to be the list.
var ErrShort = errors.New("the anime list holds too few entries")

// Fetch reads both published lists into entries.
func Fetch(ctx context.Context) ([]Entry, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	places, err := fetch(ctx, client, PlacesURL)
	if err != nil {
		return nil, err
	}
	defer places.Close()
	ids, err := fetch(ctx, client, IDsURL)
	if err != nil {
		return nil, err
	}
	defer ids.Close()
	entries, err := Read(io.LimitReader(places, maxList), io.LimitReader(ids, maxList))
	if err == nil && len(entries) < minEntries {
		err = fmt.Errorf("%w: %d", ErrShort, len(entries))
	}
	return entries, err
}

func fetch(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}
