package release

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

// ReleaseIDPrefix prefixes every release ID so an ID is self-describing in logs, CLI
// output and proof, and cannot be confused with a request ID or a commit SHA.
const ReleaseIDPrefix = "rel_"

// releaseIDBodyLen is the length of the ULID body: 26 Crockford base32 characters
// encoding 128 bits.
const releaseIDBodyLen = 26

// crockford is Crockford's base32 alphabet: no I, L, O or U, so a human reading an ID
// off a projector cannot transcribe it wrong.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ReleaseID is a Release's stable identity.
//
// # Why a ULID, and not a content-derived ID
//
// A content-derived ID (hash of target + merge commit + digest) was rejected. Two
// submissions of the same evidence for the same target are two distinct release
// events: a retry after a withheld decision, a redeploy after an abort, or a second
// canary of the same artifact each need their own decision, their own timestamps and
// their own proof. A content-derived ID would make exactly those collide - the case
// the specification says must not collide - and it would make a Release's identity
// depend on evidence that is not yet verified at the moment the ID must be assigned.
//
// A ULID gives:
//
//   - assignment before any verification, rendering, or eligibility step, because it
//     depends on nothing but the clock and entropy. #48 requires the record to be
//     persisted with a stable ID before Release Eligibility is recorded;
//   - 80 bits of cryptographic randomness per millisecond, so IDs do not collide
//     across releases for the same target;
//   - lexicographic order that matches creation order, so a listing sorts correctly
//     in any store without a secondary index;
//   - a fixed 26-character body with no separators, so it is safe in a filename, a
//     URL path, and a Kubernetes annotation value.
//
// A ULID also embeds its creation time, which is convenient but is *not* treated as
// authoritative: the Release carries its own CreatedAt.
//
// # Why it never reaches rendered bytes
//
// A release ID contains a timestamp and randomness, so it must never appear in the
// Rendered Manifest Bundle - if it did, rendering would not be deterministic. The
// renderer's signature does not accept a ReleaseID at all, which makes that structural
// rather than a rule to remember. See github.com/AndrewMaged814/safelane/internal/render.
type ReleaseID string

// NewReleaseID mints an ID from a timestamp and an entropy source. Both are arguments
// so tests are deterministic.
func NewReleaseID(t time.Time, entropy io.Reader) (ReleaseID, error) {
	ms := t.UTC().UnixMilli()
	if ms < 0 {
		return "", Internal("invalid_release_timestamp", fmt.Sprintf("timestamp %s precedes the unix epoch", t))
	}
	var raw [16]byte
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	if _, err := io.ReadFull(entropy, raw[6:]); err != nil {
		return "", Internal("entropy_unavailable", "could not read randomness for a release id").WithCause(err)
	}
	return ReleaseID(ReleaseIDPrefix + encodeULID(raw)), nil
}

// MintReleaseID mints an ID from the wall clock and crypto/rand.
func MintReleaseID() (ReleaseID, error) { return NewReleaseID(time.Now(), rand.Reader) }

// ParseReleaseID validates an ID received from a caller, for example the argument to
// `safelane proof <release-id>`.
func ParseReleaseID(s string) (ReleaseID, error) {
	bad := func(msg string) error {
		return Invalid("malformed_release_id", "release_id", msg,
			fmt.Sprintf("Use the release id SafeLane returned, of the form %s followed by %d Crockford base32 characters.", ReleaseIDPrefix, releaseIDBodyLen))
	}
	body, ok := strings.CutPrefix(s, ReleaseIDPrefix)
	if !ok {
		return "", bad(fmt.Sprintf("%q does not start with %q", s, ReleaseIDPrefix))
	}
	if len(body) != releaseIDBodyLen {
		return "", bad(fmt.Sprintf("%q is %d characters long, expected %d", body, len(body), releaseIDBodyLen))
	}
	if body[0] > '7' {
		return "", bad(fmt.Sprintf("%q overflows 128 bits", body))
	}
	for i := 0; i < len(body); i++ {
		if crockfordIndex(body[i]) < 0 {
			return "", bad(fmt.Sprintf("%q contains %q, which is not a Crockford base32 character", body, body[i]))
		}
	}
	return ReleaseID(s), nil
}

// Validate reports whether the ID is well formed.
func (id ReleaseID) Validate() error {
	_, err := ParseReleaseID(string(id))
	return err
}

// Time returns the creation time embedded in the ID. It is convenience for sorting and
// debugging; [Release.CreatedAt] is the authoritative timestamp.
func (id ReleaseID) Time() (time.Time, error) {
	if err := id.Validate(); err != nil {
		return time.Time{}, err
	}
	raw, err := decodeULID(string(id)[len(ReleaseIDPrefix):])
	if err != nil {
		return time.Time{}, err
	}
	ms := int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 | int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
	return time.UnixMilli(ms).UTC(), nil
}

func (id ReleaseID) String() string { return string(id) }

func encodeULID(raw [16]byte) string {
	hi := binary.BigEndian.Uint64(raw[0:8])
	lo := binary.BigEndian.Uint64(raw[8:16])
	var out [releaseIDBodyLen]byte
	for i := releaseIDBodyLen - 1; i >= 0; i-- {
		out[i] = crockford[lo&0x1f]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}

func decodeULID(body string) ([16]byte, error) {
	var raw [16]byte
	var hi, lo uint64
	for i := 0; i < len(body); i++ {
		v := crockfordIndex(body[i])
		if v < 0 {
			return raw, Internal("malformed_release_id", fmt.Sprintf("%q is not Crockford base32", body))
		}
		hi = hi<<5 | lo>>59
		lo = lo<<5 | uint64(v)
	}
	binary.BigEndian.PutUint64(raw[0:8], hi)
	binary.BigEndian.PutUint64(raw[8:16], lo)
	return raw, nil
}

func crockfordIndex(c byte) int {
	for i := 0; i < len(crockford); i++ {
		if crockford[i] == c {
			return i
		}
	}
	return -1
}
