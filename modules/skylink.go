package modules

// skylink.go creates links that can be used to reference data stored within a
// sector on Uplo. The skylinks can point to the full sector, as well as
// subsections of a sector.

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"math/bits"
	"strings"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"

	"github.com/uplo-tech/errors"
)

const (
	// SkylinkMaxFetchSize defines the maximum fetch size that is supported by
	// the skylink format. This is intentionally the same number as
	// modules.SectorSize on the release build. We could not use
	// modules.SectorSize directly because during testing that value is too
	// small to properly test the link format.
	SkylinkMaxFetchSize = 1 << 22

	// base32EncodedSkylinkSize is the size of the Skylink after it has been
	// encoded using base32.
	base32EncodedSkylinkSize = 55

	// base64EncodedSkylinkSize is the size of the Skylink after it has been
	// encoded using base64.
	base64EncodedSkylinkSize = 46

	// rawSkylinkSize is the raw size of the data that gets put into a link.
	rawSkylinkSize = 34
)

var (
	// ErrSkylinkIncorrectSize is returned when a string could not be decoded
	// into a Skylink due to it having an incorrect size.
	ErrSkylinkIncorrectSize = errors.New("skylink has incorrect size")
)

type (
	// Skylink contains all of the information that can be encoded into a
	// skylink. This information consists of a 32 byte MerkleRoot and a 2 byte
	// bitfield.
	//
	// The first two bits of the bitfield (values 1 and 2 in decimal) determine
	// the version of the skylink. The skylink version determines how the
	// remaining bits are used. Not all values of the bitfield are legal.
	Skylink struct {
		bitfield   uint16
		merkleRoot crypto.Hash
	}
)

// NewSkylinkV1 will return a v1 Skylink object with the version set to 1 and
// the remaining fields set appropriately. Note that the offset needs to be
// aligned correctly. Check OffsetAndFetchSize for a full list of rules on legal
// offsets - the value of a legal offset depends on the provided length.
//
// The input length will automatically be converted to the nearest fetch size.
func NewSkylinkV1(merkleRoot crypto.Hash, offset, length uint64) (Skylink, error) {
	var sl Skylink
	err := sl.setOffsetAndFetchSize(offset, length)
	if err != nil {
		return Skylink{}, errors.AddContext(err, "Invalid Skylink")
	}
	sl.merkleRoot = merkleRoot
	return sl, nil
}

// isSkylinkV1 returns a boolean indicating if the Skylink is a V1 skylink
func isSkylinkV1(bitfield uint16) bool {
	return bitfield&3 == 0
}

// validateAndParseV1Bitfield is a helper method which validates that a bitfield
// is valid and also parses the offset and fetch size from the bitfield. These
// two actions are performed at once because performing full validation requires
// parsing the bitfield, but parsing the bitfield cannot be done without partial
// validation. Cleanest way to handle this seemed to be to do them both
// together.
func validateAndParseV1Bitfield(bitfield uint16) (offset uint64, fetchSize uint64, err error) {
	// Parse the version and ensure that the version is set to '1'. The first
	// two bits are the version bits, and semantically are upshifted. So '00'
	// corresponds to version 1, '01' corresponds to version 2, up to version 4.
	if !isSkylinkV1(bitfield) {
		return 0, SkylinkMaxFetchSize, errors.New("bitfield is not set to v1")
	}
	// Shift out the version bits.
	bitfield >>= 2

	// The v1 bitfield has a requirement that there is at least one '0' in the
	// bitrange 3-10. Each consecutive '1' bit starting from the 3rd index
	// indicates that the next mode of the data structure should be used.  After
	// 8 consecutive 1's, there are no more valid modes of the data structure
	// available, and the bitfield is invalid.
	if bitfield&255 == 255 {
		return 0, SkylinkMaxFetchSize, errors.New("skylink is not valid, offset and fetch size are illegal")
	}

	// Determine how many mode bits are set.
	modeBits := uint16(bits.TrailingZeros16(^bitfield))
	// A number of 8 or larger is illegal.
	if modeBits > 7 {
		return 0, SkylinkMaxFetchSize, errors.New("bitfield has invalid mode bits")
	}
	// Shift the mode bits out. The mode bits is the number of trailing '1's
	// plus an additional '0' which is necessary to signal the end of the mode
	// bits.
	bitfield >>= modeBits + 1

	// Determine the offset alignment and the step size. The offset alignment
	// starts at 4kb and doubles once per modeBit.
	offsetAlign := uint64(4096)
	fetchSizeAlign := uint64(4096)
	offsetAlign <<= modeBits
	if modeBits > 0 {
		fetchSizeAlign <<= modeBits - 1
	}

	// The next three bits decide the fetch size.
	fetchSize = uint64(bitfield & 7)
	fetchSize++ // semantic upstep, cover the range [1, 8] instead of [0, 8).
	fetchSize *= fetchSizeAlign
	if modeBits > 0 {
		fetchSize += fetchSizeAlign << 3
	}
	bitfield >>= 3

	// The remaining bits decide the offset.
	offset = uint64(bitfield) * offsetAlign
	if offset+fetchSize > SkylinkMaxFetchSize {
		return 0, SkylinkMaxFetchSize, errors.New("invalid bitfield, fetching beyond the limits of the sector")
	}
	return offset, fetchSize, nil
}

// Bitfield returns the bitfield of a skylink.
func (sl *Skylink) Bitfield() uint16 {
	return sl.bitfield
}

// Bytes returns the raw bytes representation of a Skylink
func (sl *Skylink) Bytes() []byte {
	raw := make([]byte, rawSkylinkSize)
	binary.LittleEndian.PutUint16(raw, sl.bitfield)
	copy(raw[2:], sl.merkleRoot[:])
	return raw
}

// DataSourceID returns a resource ID for the Skylink. This ID is typically used
// inside of the renter to uniquely identify a stream buffer.
func (sl Skylink) DataSourceID() DataSourceID {
	return DataSourceID(crypto.HashObject(sl.String()))
}

// IsSkylinkV1 returns a boolean indicating if the Skylink is a V1 skylink
func (sl Skylink) IsSkylinkV1() bool {
	return isSkylinkV1(sl.bitfield)
}

// LoadString converts from a string and loads the result into sl.
func (sl *Skylink) LoadString(s string) error {
	// Trim any parameters that may exist after a question mark. Eventually, it
	// will be possible to parse these separately as additional/optional
	// arguments, for now anything after a question mark is just ignored.
	splits := strings.SplitN(s, "?", 2)
	// No need to check if there is an element returned by strings.SplitN, so
	// long as the second arg is not-nil (in this case, '?'), SplitN cannot
	// return an empty slice.
	base := splits[0]

	// The base can still contain a path to a nested file within the uplofile.
	// This is however not part of the skylink and gets trimmed.
	splits = strings.Split(base, "/")
	if len(splits) > 1 {
		base = splits[0]
	}

	// Decode the base into raw data
	raw, err := decodeSkylink(base)
	if err != nil {
		return errors.AddContext(err, "unable to decode skylink")
	}

	// Load the raw data
	return sl.LoadBytes(raw)
}

// MerkleRoot returns the merkle root of the Skylink.
func (sl Skylink) MerkleRoot() crypto.Hash {
	return sl.merkleRoot
}

// OffsetAndFetchSize returns the offset and fetch size of a file that sits
// within a skylink sector. All skylinks point to one sector of data. If the
// file is large enough that more data is necessary, a "fanout" is used to point
// to more sectors.
//
// NOTE: To fully understand the bitfield of the v1 Skylink, it is recommended
// that the following documentation is read alongside the code.
//
// Sectors are 4 MiB large. To enable the support of efficiently storing and
// downloading smaller files, the skylink allows an offset and a fetch size to
// be specified for a file, which means many files can be stored within a single
// sector root, and each file can get a unique 46 byte skylink.
//
// Existing content addressing systems use 46 bytes, to maximize compatibility
// we have also chosen to adhere to a 46 byte link size. 46 bytes of base64 is
// 34 bytes of raw data, which means there are only 34 bytes to work with for
// storing extra information such as the version, offset, and fetch size of a
// file. The tight data constraints resulted in this compact format.
//
// Skylinks are given 2 bits for a version. These bits are always the first 2
// bits of the bitfield, which correspond to the values '1' and '2' when the
// bitfield is interpreted as a uint16. The version must be set to 1 to retrieve
// an offset and a fetch size.
//
// That leaves 14 bits to determine the actual offset and fetch size for the
// file.  The first 8 of those 14 bits are conditional bits, operating somewhat
// like varints. There are 8 total "modes" that can be triggered by these 8
// bits.  The first mode is triggered if the first of the 8 bits is a "0". That
// mode indicates that the 13 remaining bits should be used to compute the
// offset and fetch size using mode 1. If the first of the 8 bits is a "1", it
// means check the next bit. If that next bit is a "0", the second mode is
// triggered, meaning that the remaining 12 bits should be used to compute the
// offset and fetch size using mode 2.
//
// Out of the 8 modes total, each mode has 1 fewer bit than the previous mode
// for computing the offset and fetch size. The first mode has 13 bits total,
// and the final mode has 6 bits total. The first three of these bits always
// indicates the fetch size. More on that later.
//
// The modes themselves are fairly simple. The first mode indicates that the
// file is stored on an offset that is aligned to 4096 (1 << 12) bytes. With
// that alignment, there are 1024 possible offsets for the file to start at
// within a 4 MiB sector. That takes 10 bits to represent with perfect
// precision, and is conveniently the number of remaining bits to determine the
// offset after the fetch size has been parsed.
//
// The second mode indicates that the file is stored on an offset that is
// aligned to 8192 (1 << 13) bytes, which means there are 512 possible offsets.
// Because a bit was consumed to switch modes, only 9 bits are available to
// indicate what the offset is. But as there are only 512 possible offsets, only
// 9 bits are needed.
//
// This continues until the final mode, which indicates that the file is stored
// on an offset that is aligned to 512 kib (1 << 19). This is where it stops,
// larger offsets are unnecessary. Having 8 consecutive 1's in a v1 Skylink is
// invalid, which means means there are 64 total unused states (all states where
// the first 8 of 14 non-version bits are set to '1').
//
// The fetch size is an upper bound that says 'the file is no more than this
// many bytes', and tells the client to download that many bytes to get the
// whole file. The actual length of the file is in the metadata that gets
// downloaded along with the file.
//
// For every mode, there are 8 possible fetch sizes. For the first mode, the
// first possible fetch size is 4 kib, and each additional possible fetch size
// is another 4 kib. That means files in the first mode can be placed on any
// 4096 byte aligned offset within the Merkle root and can be up to 32 kib
// large.
//
// For the second mode, the fetch sizes also increase by 4 kib at a time,
// starting where the first mode left off. The smallest fetch size that a file
// in the second mode can have is 36 kib, and the largest fetch size that a file
// in the second mode can have is 64 kib.
//
// Each mode after that, the increment of the fetch size doubles. So the third
// mode starts at a fetch size of 72 kib, and goes up to a fetch size of 128
// kib. And the fourth mode starts at a fetch size of 144 kib, and goes up to a
// fetch size of 256 kib. The eighth and final mode extends up to a fetch size
// of 4 MiB, which is the full size of the sector.
//
// A full table of fetch sizes is presented here:
//
//	   4,    8,   12,   16,   20,   24,   28,   32,
//	  36,   40,   44,   48,   52,   56,   60,   64,
//	  72,   80,   88,   96,  104,  112,  120,  128,
//	 144,  160,  176,  192,  208,  224,  240,  256,
//	 288,  320,  352,  384,  416,  448,  480,  512,
//	 576,  640,  704,  768,  832,  896,  960, 1024,
//	1152, 1280, 1408, 1536, 1664, 1792, 1920, 2048,
//	2304, 2560, 2816, 3072, 3328, 3584, 3840, 4096,
//
// Certain combinations of offset + fetch size are illegal. Specifically, it is
// illegal to indicate a fetch size that goes beyond the boundary of the file.
// The first mode has 28 illegal states, and each mode after that has 60 illegal
// states. Combined with the 64 illegal states that can be created by
// incorrectly set mode bits, there are 512 illegal states total for v1 of the
// Uplo link.
//
// It's possible that these states will be repurposed in the future, extending
// the functionality of the v1 skylink. More likely however, a transition to v2
// will be made instead.
//
// NOTE: If there is an error, OffsetAndLen will return a signal to download the
// entire sector. This means that any code which is ignoring the error will
// still have mostly sane behavior.
func (sl Skylink) OffsetAndFetchSize() (offset uint64, fetchSize uint64, err error) {
	return validateAndParseV1Bitfield(sl.bitfield)
}

// String converts Skylink to a string.
func (sl Skylink) String() string {
	// Encode the raw bytes to base64.
	return base64.RawURLEncoding.EncodeToString(sl.Bytes())
}

// Version will pull the version out of the bitfield and return it. The version
// is a 2 bit number, meaning there are 4 possible values. The bitwise values
// cover the range [0, 3], however we want to return a value in the range
// [1, 4], so we increment the bitwise result.
func (sl Skylink) Version() uint16 {
	return (sl.bitfield & 3) + 1
}

// LoadBytes loads the given raw data onto the skylink.
func (sl *Skylink) LoadBytes(data []byte) error {
	// Sanity check the size of the given data
	if len(data) != rawSkylinkSize {
		build.Critical("raw skylink data has the incorrect size")
		return errors.New("failed to load skylink data")
	}

	// Load and check the bitfield. The bitfield is checked before modifying the
	// Skylink so that the Skylink remains unchanged if there is any error
	// parsing the string.
	bitfield := binary.LittleEndian.Uint16(data)
	_, _, err := validateAndParseV1Bitfield(bitfield)
	if err != nil {
		return errors.AddContext(err, "skylink failed verification")
	}

	// Load the raw data.
	sl.bitfield = bitfield
	copy(sl.merkleRoot[:], data[2:])
	return nil
}

// setOffsetAndFetchSize will set the offset and fetch size of the data within
// the skylink. Offset must be aligned correctly. setOffsetAndLen implies that
// the version is 1, so the version will also be set to 1.
func (sl *Skylink) setOffsetAndFetchSize(offset, fetchSize uint64) error {
	if offset+fetchSize > SkylinkMaxFetchSize {
		return errors.New("offset plus fetch size cannot exceed the size of one sector - 4 MiB")
	}

	// Given the fetch size, determine the appropriate offset alignment.
	//
	// The largest offset alignment is 512 kib, which is used if the fetch size
	// is 2 MiB or over. Each time the fetch size is halved, the offset
	// alignment is also halved. The smallest offset alignment is 4 kib.
	minFetchSize := uint64(1 << 21)
	offsetAlign := uint64(1 << 19)
	for fetchSize <= minFetchSize && offsetAlign > (1<<12) {
		offsetAlign >>= 1
		minFetchSize >>= 1
	}
	if offset&(offsetAlign-1) != 0 {
		return errors.New("offset is not aligned correctly")
	}
	// The bitwise representation of the offset is the actual offset divided by
	// the offset alignment.
	bitwiseOffset := uint16(offset / offsetAlign)

	// Unless the offsetAlign is 1 << 12, the fetch size alignment is 1/2 the
	// offsetAlign. If the offsetAlign is 1 << 12, the fetch size alignment is
	// also 1 << 12.
	fetchSizeAlign := uint64(1 << 12)
	if offsetAlign > 1<<13 {
		fetchSizeAlign = offsetAlign >> 1
	}
	// If the the mode is anything besides the first mode, there is a fetch size
	// shift by 8 times the fetch size. We know that the mode is not the first
	// mode if the offsetAlign is not 1 << 12
	if offsetAlign > 1<<12 {
		fetchSize = fetchSize - fetchSizeAlign*8
	}
	// Round the fetch size to the fetchSizeAlign. Because the fetch size is
	// semantically shifted from the range [0, 8) to [1, 8], we round down. If
	// the fetch size is already evenly divisible by the fetchSizeAlign, it's
	// not going to be rounded down so it needs to be decremented manually.
	if fetchSize != 0 && fetchSize == (fetchSize/fetchSizeAlign)*fetchSizeAlign {
		fetchSize--
	}
	fetchSize = fetchSize & (^(fetchSizeAlign - 1))
	bitwiseFetchSize := uint16(fetchSize / fetchSizeAlign)

	// Add the offset to the bitfield.
	bitfield := bitwiseOffset
	// Shift the bitfield up to add the fetch size.
	bitfield <<= 3
	bitfield += bitwiseFetchSize
	// Shift the bitfield up to add the 0 bit that terminates the mode bits.
	bitfield <<= 1
	// Shift in all of the mode bits.
	baseAlign := uint64(1 << 12)
	for baseAlign < offsetAlign {
		baseAlign <<= 1
		bitfield <<= 1
		bitfield += 1
	}
	// Shift 2 more bits for the version, this should now be 16 bits total. The
	// two final bits for the version are both kept at 0 to siganl version 1.
	bitfield <<= 2

	// Set the bitfield and return.
	sl.bitfield = bitfield
	return nil
}

// decodeSkylink is a helper function that decodes the given string
// representation of a skylink  into raw bytes. It either performs a base32
// decoding, or base64 decoding, depending on the length.
func decodeSkylink(encoded string) ([]byte, error) {
	switch len(encoded) {
	case base32EncodedSkylinkSize:
		return base32.HexEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(encoded))
	case base64EncodedSkylinkSize:
		return base64.RawURLEncoding.DecodeString(encoded)
	default:
		return nil, ErrSkylinkIncorrectSize
	}
}
