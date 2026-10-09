package kv

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// trackerLinks are the codes trackers gave profiles, waiting to be entered: one hash, so every
// script touches the single key it declares, as Valkey Cluster requires, with a field for each
// profile and tracker that expires with its code.
const trackerLinks = "tracker_links"

// TrackerLink is a tracker's code for a profile to enter at VerificationURI, or at
// VerificationURIComplete, which has the code in it already. DeviceCode is what the tracker is
// asked after it by, and never leaves the server.
type TrackerLink struct {
	Profile                 uuid.UUID      `json:"profile"`
	Tracker                 domain.Tracker `json:"tracker"`
	DeviceCode              string         `json:"device_code"`
	UserCode                string         `json:"user_code"`
	VerificationURI         string         `json:"verification_uri"`
	VerificationURIComplete string         `json:"verification_uri_complete"`
	Interval                time.Duration  `json:"interval"`
	Expires                 time.Time      `json:"expires"`
	// Asked is when the tracker was last asked after it, in Valkey's clock's milliseconds.
	Asked int64 `json:"asked"`
}

func linkField(profile uuid.UUID, t domain.Tracker) string { return profile.String() + ":" + string(t) }

var startLink = valkey.NewLuaScript(`
local t = redis.call('TIME')
local l = cjson.decode(ARGV[2])
l.asked = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
redis.call('HSETEX', KEYS[1], 'PX', ARGV[3], 'FIELDS', 1, ARGV[1], cjson.encode(l))`)

// StartTrackerLink holds l, in place of any link its profile was making on its tracker, until it
// expires. The tracker is first asked after it an interval from now.
func (k *KV) StartTrackerLink(ctx context.Context, l TrackerLink) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	err = startLink.Exec(ctx, k.client, []string{k.key(trackerLinks)}, []string{
		linkField(l.Profile, l.Tracker), string(b), strconv.FormatInt(time.Until(l.Expires).Milliseconds(), 10),
	}).Error()
	if valkey.IsValkeyNil(err) {
		return nil
	}
	return err
}

// TrackerLink answers the link a profile is making on a tracker, if any.
func (k *KV) TrackerLink(ctx context.Context, profile uuid.UUID, t domain.Tracker) (TrackerLink, bool, error) {
	var l TrackerLink
	b, err := k.client.Do(ctx, k.client.B().Hget().Key(k.key(trackerLinks)).Field(linkField(profile, t)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return l, false, nil
	}
	if err != nil {
		return l, false, err
	}
	return l, true, json.Unmarshal(b, &l)
}

// dueLinks claims each link whose interval has passed since its tracker was last asked after it,
// so whichever nodes look, each tracker is asked once each interval.
var dueLinks = valkey.NewLuaScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local due = {}
local all = redis.call('HGETALL', KEYS[1])
for i = 1, #all, 2 do
  local l = cjson.decode(all[i + 1])
  if now - l.asked >= l.interval / 1000000 then
    l.asked = now
    local b = cjson.encode(l)
    redis.call('HSETEX', KEYS[1], 'KEEPTTL', 'FIELDS', 1, all[i], b)
    table.insert(due, b)
  end
end
return due`)

// DueTrackerLinks claims the links whose trackers are the caller's to ask after them now.
func (k *KV) DueTrackerLinks(ctx context.Context) ([]TrackerLink, error) {
	vals, err := dueLinks.Exec(ctx, k.client, []string{k.key(trackerLinks)}, nil).AsStrSlice()
	if err != nil {
		return nil, err
	}
	out := make([]TrackerLink, len(vals))
	for i, v := range vals {
		if err := json.Unmarshal([]byte(v), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

var slowLink = valkey.NewLuaScript(`
local b = redis.call('HGET', KEYS[1], ARGV[1])
if not b then return end
local l = cjson.decode(b)
l.interval = l.interval + tonumber(ARGV[2])
redis.call('HSETEX', KEYS[1], 'KEEPTTL', 'FIELDS', 1, ARGV[1], cjson.encode(l))`)

// SlowTrackerLink asks a link's tracker after it less often, by, as the tracker asked.
func (k *KV) SlowTrackerLink(ctx context.Context, profile uuid.UUID, t domain.Tracker, by time.Duration) error {
	err := slowLink.Exec(ctx, k.client, []string{k.key(trackerLinks)}, []string{linkField(profile, t), strconv.FormatInt(int64(by), 10)}).Error()
	if valkey.IsValkeyNil(err) {
		return nil
	}
	return err
}

// EndTrackerLink forgets the link a profile was making on a tracker, answering whether there was
// one.
func (k *KV) EndTrackerLink(ctx context.Context, profile uuid.UUID, t domain.Tracker) (bool, error) {
	n, err := k.client.Do(ctx, k.client.B().Hdel().Key(k.key(trackerLinks)).Field(linkField(profile, t)).Build()).AsInt64()
	return n == 1, err
}
