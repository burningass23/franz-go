// Package kmsg contains Kafka request and response types and autogenerated
// serialization and deserialization functions.
//
// This package may bump major versions whenever Kafka makes a backwards
// incompatible protocol change, per the types chosen for this package. For
// example, Kafka can change a field from non-nullable to nullable, which would
// require changing a field from a non-pointer to a pointer. We could get
// around this by making everything an opaque struct and having getters, but
// that is more tedious than having a few rare major version bumps.
//
// If you are using this package directly with kgo, you should either always
// use New functions, or Default functions after creating structs, or you
// should pin the max supported version. If you use New functions, you will
// have safe defaults as new fields are added. If you pin versions, you will
// avoid new fields being used. If you do neither of these, you may opt in to
// new fields that do not have safe zero value defaults, and this may lead to
// errors or unexpected results.
//
// Thus, whenever you initialize a struct from this package, do the following:
//
//	struct := kmsg.NewFoo()
//	struct.Field = "value I want to set"
//
// Most of this package is generated, but a few things are manual. What is
// manual: all interfaces, the RequestFormatter, record / message / record
// batch reading, and sticky member metadata serialization.
package kmsg

import (
	"context"
	"sort"

	"github.com/burningass23/franz-go/pkg/kmsg/internal/kbin"
)

//go:generate cp ../kbin/primitives.go internal/kbin/

// Requestor issues requests. Notably, the kgo.Client and kgo.Broker implements
// Requestor. All Requests in this package have a RequestWith function to have
// type-safe requests.
type Requestor interface {
	// Request issues a Request and returns either a Response or an error.
	Request(context.Context, Request) (Response, error)
}

// Request represents a type that can be requested to Kafka.
type Request interface {
	// Key returns the protocol key for this message kind.
	Key() int16
	// MaxVersion returns the maximum protocol version this message
	// supports.
	//
	// This function allows one to implement a client that chooses message
	// versions based off of the max of a message's max version in the
	// client and the broker's max supported version.
	MaxVersion() int16
	// SetVersion sets the version to use for this request and response.
	SetVersion(int16)
	// GetVersion returns the version currently set to use for the request
	// and response.
	GetVersion() int16
	// IsFlexible returns whether the request at its current version is
	// "flexible" as per the KIP-482.
	IsFlexible() bool
	// AppendTo appends this message in wire protocol form to a slice and
	// returns the slice.
	AppendTo([]byte) []byte
	// ReadFrom parses all of the input slice into the response type.
	//
	// This should return an error if too little data is input.
	ReadFrom([]byte) error
	// ResponseKind returns an empty Response that is expected for
	// this message request.
	ResponseKind() Response
}

// AdminRequest represents a request that must be issued to Kafka controllers.
type AdminRequest interface {
	// IsAdminRequest is a method attached to requests that must be
	// issed to Kafka controllers.
	IsAdminRequest()
	Request
}

// GroupCoordinatorRequest represents a request that must be issued to a
// group coordinator.
type GroupCoordinatorRequest interface {
	// IsGroupCoordinatorRequest is a method attached to requests that
	// must be issued to group coordinators.
	IsGroupCoordinatorRequest()
	Request
}

// TxnCoordinatorRequest represents a request that must be issued to a
// transaction coordinator.
type TxnCoordinatorRequest interface {
	// IsTxnCoordinatorRequest is a method attached to requests that
	// must be issued to transaction coordinators.
	IsTxnCoordinatorRequest()
	Request
}

// Response represents a type that Kafka responds with.
type Response interface {
	// Key returns the protocol key for this message kind.
	Key() int16
	// MaxVersion returns the maximum protocol version this message
	// supports.
	MaxVersion() int16
	// SetVersion sets the version to use for this request and response.
	SetVersion(int16)
	// GetVersion returns the version currently set to use for the request
	// and response.
	GetVersion() int16
	// IsFlexible returns whether the request at its current version is
	// "flexible" as per the KIP-482.
	IsFlexible() bool
	// AppendTo appends this message in wire protocol form to a slice and
	// returns the slice.
	AppendTo([]byte) []byte
	// ReadFrom parses all of the input slice into the response type.
	//
	// This should return an error if too little data is input.
	ReadFrom([]byte) error
	// RequestKind returns an empty Request that is expected for
	// this message request.
	RequestKind() Request
}

// UnsafeReadFrom, implemented by all requests and responses generated in this
// package, switches to using unsafe slice-to-string conversions when reading.
// This can be used to avoid a lot of garbage, but it means to have to be
// careful when using any strings in structs: if you hold onto the string, the
// underlying response slice will not be garbage collected.
type UnsafeReadFrom interface {
	UnsafeReadFrom([]byte) error
}

// ThrottleResponse represents a response that could have a throttle applied by
// Kafka. Any response that implements ThrottleResponse also implements
// SetThrottleResponse.
//
// Kafka 2.0.0 switched throttles from being applied before responses to being
// applied after responses.
type ThrottleResponse interface {
	// Throttle returns the response's throttle millis value and
	// whether Kafka applies the throttle after the response.
	Throttle() (int32, bool)
}

// SetThrottleResponse sets the throttle in a response that can have a throttle
// applied. Any kmsg interface that implements ThrottleResponse also implements
// SetThrottleResponse.
type SetThrottleResponse interface {
	// SetThrottle sets the response's throttle millis value.
	SetThrottle(int32)
}

// TimeoutRequest represents a request that has a TimeoutMillis field.
// Any request that implements TimeoutRequest also implements SetTimeoutRequest.
type TimeoutRequest interface {
	// Timeout returns the request's timeout millis value.
	Timeout() int32
}

// SetTimeoutRequest sets the timeout in a request that can have a timeout
// applied. Any kmsg interface that implements ThrottleRequest also implements
// SetThrottleRequest.
type SetTimeoutRequest interface {
	// SetTimeout sets the request's timeout millis value.
	SetTimeout(timeoutMillis int32)
}

// RequestFormatter formats requests.
//
// The default empty struct works correctly, but can be extended with the
// NewRequestFormatter function.
type RequestFormatter struct {
	clientID *string
}

// RequestFormatterOpt applys options to a RequestFormatter.
type RequestFormatterOpt interface {
	apply(*RequestFormatter)
}

type formatterOpt struct{ fn func(*RequestFormatter) }

func (opt formatterOpt) apply(f *RequestFormatter) { opt.fn(f) }

// FormatterClientID attaches the given client ID to any issued request,
// minus controlled shutdown v0, which uses its own special format.
func FormatterClientID(id string) RequestFormatterOpt {
	return formatterOpt{func(f *RequestFormatter) { f.clientID = &id }}
}

// NewRequestFormatter returns a RequestFormatter with the opts applied.
func NewRequestFormatter(opts ...RequestFormatterOpt) *RequestFormatter {
	a := new(RequestFormatter)
	for _, opt := range opts {
		opt.apply(a)
	}
	return a
}

// AppendRequest appends a full message request to dst, returning the updated
// slice. This message is the full body that needs to be written to issue a
// Kafka request.
func (f *RequestFormatter) AppendRequest(
	dst []byte,
	r Request,
	correlationID int32,
) []byte {
	dst = append(dst, 0, 0, 0, 0) // reserve length
	k := r.Key()
	v := r.GetVersion()
	dst = kbin.AppendInt16(dst, k)
	dst = kbin.AppendInt16(dst, v)
	dst = kbin.AppendInt32(dst, correlationID)
	if k == 7 && v == 0 {
		return dst
	}

	// Even with flexible versions, we do not use a compact client id.
	// Clients issue ApiVersions immediately before knowing the broker
	// version, and old brokers will not be able to understand a compact
	// client id.
	dst = kbin.AppendNullableString(dst, f.clientID)

	// The flexible tags end the request header, and then begins the
	// request body.
	if r.IsFlexible() {
		var numTags uint8
		dst = append(dst, numTags)
		if numTags != 0 {
			// TODO when tags are added
		}
	}

	// Now the request body.
	dst = r.AppendTo(dst)

	kbin.AppendInt32(dst[:0], int32(len(dst[4:])))
	return dst
}

// StringPtr is a helper to return a pointer to a string.
func StringPtr(in string) *string {
	return &in
}

// ReadFrom provides decoding various versions of sticky member metadata. A key
// point of this type is that it does not contain a version number inside it,
// but it is versioned: if decoding v1 fails, this falls back to v0.
func (s *StickyMemberMetadata) ReadFrom(src []byte) error {
	return s.readFrom(src, false)
}

// UnsafeReadFrom is the same as ReadFrom, but uses unsafe slice to string
// conversions to reduce garbage.
func (s *StickyMemberMetadata) UnsafeReadFrom(src []byte) error {
	return s.readFrom(src, true)
}

func (s *StickyMemberMetadata) readFrom(src []byte, unsafe bool) error {
	b := kbin.Reader{Src: src}
	numAssignments := b.ArrayLen()
	if numAssignments < 0 {
		numAssignments = 0
	}
	need := numAssignments - int32(cap(s.CurrentAssignment))
	if need > 0 {
		s.CurrentAssignment = append(s.CurrentAssignment[:cap(s.CurrentAssignment)], make([]StickyMemberMetadataCurrentAssignment, need)...)
	} else {
		s.CurrentAssignment = s.CurrentAssignment[:numAssignments]
	}
	for i := int32(0); i < numAssignments; i++ {
		var topic string
		if unsafe {
			topic = b.UnsafeString()
		} else {
			topic = b.String()
		}
		numPartitions := b.ArrayLen()
		if numPartitions < 0 {
			numPartitions = 0
		}
		a := &s.CurrentAssignment[i]
		a.Topic = topic
		need := numPartitions - int32(cap(a.Partitions))
		if need > 0 {
			a.Partitions = append(a.Partitions[:cap(a.Partitions)], make([]int32, need)...)
		} else {
			a.Partitions = a.Partitions[:numPartitions]
		}
		for i := range a.Partitions {
			a.Partitions[i] = b.Int32()
		}
	}
	if len(b.Src) > 0 {
		s.Generation = b.Int32()
	} else {
		s.Generation = -1
	}
	return b.Complete()
}

// AppendTo provides appending various versions of sticky member metadata to dst.
// If generation is not -1 (default for v0), this appends as version 1.
func (s *StickyMemberMetadata) AppendTo(dst []byte) []byte {
	dst = kbin.AppendArrayLen(dst, len(s.CurrentAssignment))
	for _, assignment := range s.CurrentAssignment {
		dst = kbin.AppendString(dst, assignment.Topic)
		dst = kbin.AppendArrayLen(dst, len(assignment.Partitions))
		for _, partition := range assignment.Partitions {
			dst = kbin.AppendInt32(dst, partition)
		}
	}
	if s.Generation != -1 {
		dst = kbin.AppendInt32(dst, s.Generation)
	}
	return dst
}

// TagReader has is a type that has the ability to skip tags.
//
// This is effectively a trimmed version of the kbin.Reader, with the purpose
// being that kmsg cannot depend on an external package.
type TagReader interface {
	// Uvarint returns a uint32. If the reader has read too much and has
	// exhausted all bytes, this should set the reader's internal state
	// to failed and return 0.
	Uvarint() uint32

	// Span returns n bytes from the reader. If the reader has read too
	// much and exhausted all bytes this should set the reader's internal
	// to failed and return nil.
	Span(n int) []byte
}

// SkipTags skips tags in a TagReader.
func SkipTags(b TagReader) {
	for num := b.Uvarint(); num > 0; num-- {
		_, size := b.Uvarint(), b.Uvarint()
		b.Span(int(size))
	}
}

// internalSkipTags skips tags in the duplicated inner kbin.Reader.
func internalSkipTags(b *kbin.Reader) {
	for num := b.Uvarint(); num > 0; num-- {
		_, size := b.Uvarint(), b.Uvarint()
		b.Span(int(size))
	}
}

// ReadTags reads tags in a TagReader and returns the tags.
func ReadTags(b TagReader) Tags {
	var t Tags
	for num := b.Uvarint(); num > 0; num-- {
		key, size := b.Uvarint(), b.Uvarint()
		t.Set(key, b.Span(int(size)))
	}
	return t
}

// internalReadTags reads tags in a reader and returns the tags from a
// duplicated inner kbin.Reader.
func internalReadTags(b *kbin.Reader) Tags {
	var t Tags
	for num := b.Uvarint(); num > 0; num-- {
		key, size := b.Uvarint(), b.Uvarint()
		t.Set(key, b.Span(int(size)))
	}
	return t
}

// Tags is an opaque structure capturing unparsed tags.
type Tags struct {
	keyvals map[uint32][]byte
}

// Len returns the number of keyvals in Tags.
func (t *Tags) Len() int { return len(t.keyvals) }

// Each calls fn for each key and val in the tags.
func (t *Tags) Each(fn func(uint32, []byte)) {
	if len(t.keyvals) == 0 {
		return
	}
	// We must encode keys in order. We expect to have limited (no) unknown
	// keys, so for now, we take a lazy approach and allocate an ordered
	// slice.
	ordered := make([]uint32, 0, len(t.keyvals))
	for key := range t.keyvals {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, key := range ordered {
		fn(key, t.keyvals[key])
	}
}

// Set sets a tag's key and val.
//
// Note that serializing tags does NOT check if the set key overlaps with an
// existing used key. It is invalid to set a key used by Kafka itself.
func (t *Tags) Set(key uint32, val []byte) {
	if t.keyvals == nil {
		t.keyvals = make(map[uint32][]byte)
	}
	t.keyvals[key] = val
}

// AppendEach appends each keyval in tags to dst and returns the updated dst.
func (t *Tags) AppendEach(dst []byte) []byte {
	t.Each(func(key uint32, val []byte) {
		dst = kbin.AppendUvarint(dst, key)
		dst = kbin.AppendUvarint(dst, uint32(len(val)))
		dst = append(dst, val...)
	})
	return dst
}
