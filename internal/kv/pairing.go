package kv

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

// A pairing is one hash, named by its user code so every script touches the single key it
// declares, as Valkey Cluster requires. It holds the hash of the device's secret, what asked and,
// once approved, for whom.
func (k *KV) pairingKey(userCode string) string { return k.key("pair:" + userCode) }

var ErrUserCodeTaken = errors.New("that user code is in use")

type Pairing struct {
	Device string
	Client string
	// Style is how its user code is written, which auth names.
	Style   string
	Profile uuid.UUID
}

type PairingState string

const (
	PairingPending  PairingState = "pending"
	PairingSlowDown PairingState = "slow_down"
	PairingExpired  PairingState = "expired"
	PairingApproved PairingState = "approved"
)

var start = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('HSET', KEYS[1], 'secret', ARGV[1], 'device', ARGV[2], 'client', ARGV[3], 'style', ARGV[5])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1`)

func (k *KV) StartPairing(ctx context.Context, userCode string, secretHash []byte, p Pairing, ttl time.Duration) error {
	created, err := start.Exec(ctx, k.client, []string{k.pairingKey(userCode)}, []string{
		hex.EncodeToString(secretHash), p.Device, p.Client, strconv.Itoa(int(ttl.Seconds())), p.Style,
	}).AsInt64()
	if err != nil {
		return err
	}
	if created == 0 {
		return ErrUserCodeTaken
	}
	return nil
}

// approve names the profile once: a pairing already approved is not approved again.
var approve = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return false end
if redis.call('HEXISTS', KEYS[1], 'profile') == 1 then return false end
redis.call('HSET', KEYS[1], 'profile', ARGV[1])
return redis.call('HMGET', KEYS[1], 'device', 'client')`)

// ApprovePairing gives the pairing waiting under userCode to profile, answering what asked.
// ok is false for a code that is unknown, expired or already approved.
func (k *KV) ApprovePairing(ctx context.Context, userCode string, profile uuid.UUID) (Pairing, bool, error) {
	vals, err := approve.Exec(ctx, k.client, []string{k.pairingKey(userCode)}, []string{profile.String()}).AsStrSlice()
	if valkey.IsValkeyNil(err) {
		return Pairing{}, false, nil
	}
	if err != nil || len(vals) != 2 {
		return Pairing{}, false, err
	}
	return Pairing{Device: vals[0], Client: vals[1], Profile: profile}, true, nil
}

var status = valkey.NewLuaScript(`
if redis.call('HGET', KEYS[1], 'secret') ~= ARGV[1] then return false end
local fields = redis.call('HMGET', KEYS[1], 'device', 'client', 'style', 'profile')
return {fields[1], fields[2], fields[3] or '', fields[4] or '', tostring(redis.call('PTTL', KEYS[1]))}`)

// PairingStatus reads the pairing a device holds the secret of as a poll would, but neither hands
// it out nor counts as a poll. remaining is how long the pairing has left.
func (k *KV) PairingStatus(ctx context.Context, userCode string, secretHash []byte) (state PairingState, p Pairing, remaining time.Duration, err error) {
	vals, err := status.Exec(ctx, k.client, []string{k.pairingKey(userCode)}, []string{hex.EncodeToString(secretHash)}).AsStrSlice()
	if valkey.IsValkeyNil(err) {
		return PairingExpired, Pairing{}, 0, nil
	}
	if err != nil || len(vals) != 5 {
		return "", Pairing{}, 0, err
	}
	ms, err := strconv.ParseInt(vals[4], 10, 64)
	if err != nil {
		return "", Pairing{}, 0, err
	}
	p, remaining = Pairing{Device: vals[0], Client: vals[1], Style: vals[2]}, time.Duration(ms)*time.Millisecond
	if vals[3] == "" {
		return PairingPending, p, remaining, nil
	}
	p.Profile, err = uuid.Parse(vals[3])
	return PairingApproved, p, remaining, err
}

// poll answers the device holding the pairing's secret, refusing one that asks more often than
// every interval, and hands an approved pairing out once.
var poll = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return {'expired'} end
if redis.call('HGET', KEYS[1], 'secret') ~= ARGV[1] then return {'expired'} end
local now = tonumber(redis.call('TIME')[1])
local last = tonumber(redis.call('HGET', KEYS[1], 'polled') or '0')
redis.call('HSET', KEYS[1], 'polled', now)
if now - last < tonumber(ARGV[2]) then return {'slow_down'} end
local profile = redis.call('HGET', KEYS[1], 'profile')
if not profile then return {'pending'} end
local fields = redis.call('HMGET', KEYS[1], 'device', 'client')
redis.call('DEL', KEYS[1])
return {'approved', profile, fields[1], fields[2]}`)

// PollPairing answers a device asking after its pairing. A wrong secret reads as expired, so a
// guessed user code reveals nothing.
func (k *KV) PollPairing(ctx context.Context, userCode string, secretHash []byte, interval time.Duration) (PairingState, Pairing, error) {
	vals, err := poll.Exec(ctx, k.client, []string{k.pairingKey(userCode)},
		[]string{hex.EncodeToString(secretHash), strconv.Itoa(int(interval.Seconds()))}).AsStrSlice()
	if err != nil || len(vals) == 0 {
		return "", Pairing{}, err
	}
	state := PairingState(vals[0])
	if state != PairingApproved {
		return state, Pairing{}, nil
	}
	profile, err := uuid.Parse(vals[1])
	return state, Pairing{Profile: profile, Device: vals[2], Client: vals[3]}, err
}
