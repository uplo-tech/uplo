package registry

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
)

// List of supported signature algorithms.
const (
	signatureEd25519 = 1
)

// noKey is a sentinel value for persisted entries. A persisted entry with the
// default key is considered to not be in use. This means the disk space can be
// reclaimed.
var noKey = compressedPublicKey{}

// registryVersion is the version at the beginning of the registry on disk
// for future compatibility changes.
var registryVersion = types.NewSpecifier("1.0.0")

// persistedEntryType is the type of the entry. Right now there is only a single
// type and that's 1.
var persistedEntryType = uint8(1)

type (
	// pesistedEntry is an entry
	// Size on disk: (1 + 32) + 32 + 4 + 1 + 113 + 8 + 64 + 1 = 256
	persistedEntry struct {
		// key data
		Key   compressedPublicKey
		Tweak [modules.TweakSize]byte

		// value data
		Expiry    compressedBlockHeight
		DataLen   uint8
		Data      [modules.RegistryDataSize]byte
		Revision  uint64
		Signature crypto.Signature

		// utility fields
		// Type is the type of the entry. Right now only a single one exists
		// which will probably change in the future.
		Type uint8
	}

	// compressedPublicKey is a version of the types.UploPublicKey which is
	// optimized for being stored on disk.
	compressedPublicKey struct {
		Algorithm byte
		Key       [crypto.PublicKeySize]byte
	}

	// compressedBlockHeight is a version of types.Blockheight which is half the
	// size.
	compressedBlockHeight uint32
)

// newCompressedPublicKey creates a compressed public key from a uplo public key.
func newCompressedPublicKey(spk types.UploPublicKey) (cpk compressedPublicKey, _ error) {
	if len(spk.Key) != len(cpk.Key) {
		return cpk, errors.New("newCompressedPublicKey: unsupported key length")
	}
	if spk.Algorithm != types.SignatureEd25519 {
		return cpk, errors.New("newCompressedPublicKey: unsupported signature algo")
	}
	copy(cpk.Key[:], spk.Key)
	cpk.Algorithm = signatureEd25519
	return
}

// newUploPublicKey creates a uplo public key from a compressed public key.
func newUploPublicKey(cpk compressedPublicKey) (spk types.UploPublicKey, _ error) {
	if cpk.Algorithm != signatureEd25519 {
		return spk, errors.New("newUploPublicKey: unknown signature algorithm")
	}
	spk.Algorithm = types.SignatureEd25519
	spk.Key = append([]byte{}, cpk.Key[:]...)
	return
}

// initRegistry initializes a registry at the specified path using the provided
// wal.
func initRegistry(path string, maxEntries uint64) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, modules.DefaultFilePerm)
	if err != nil {
		return nil, errors.AddContext(err, "failed to create new file for key/value store")
	}

	// Truncate the file to its max size.
	err = f.Truncate(int64(maxEntries * PersistedEntrySize))
	if err != nil {
		err = errors.Compose(err, f.Close()) // close the file on error
		return nil, errors.AddContext(err, "failed to preallocate registry disk space")
	}

	// The first entry is reserved for metadata. Right now only the version
	// number.
	initData := make([]byte, PersistedEntrySize)
	copy(initData[:], registryVersion[:])

	// Write data to disk.
	_, err = f.WriteAt(initData, 0)
	if err != nil {
		err = errors.Compose(err, f.Close()) // close the file on error
		return nil, errors.AddContext(err, "failed to write initial data to registry")
	}
	return f, f.Sync()
}

// loadRegistryMetadata tries to read the first persisted entry that contains
// the registry metadata and verifies it.
func loadRegistryMetadata(r io.Reader, b bitfield) error {
	// Check version. We only have one so far so we can compare to that
	// directly.
	var entry [PersistedEntrySize]byte
	_, err := io.ReadFull(r, entry[:])
	if err != nil {
		return errors.AddContext(err, "failed to read metadata page")
	}
	version := entry[:types.SpecifierLen]
	if !bytes.Equal(version, registryVersion[:]) {
		return fmt.Errorf("expected store version %v but got %v", registryVersion, version)
	}
	return nil
}

// loadRegistryEntries reads the currently in use registry entries from disk.
func loadRegistryEntries(r io.Reader, numEntries int64, b bitfield) (map[crypto.Hash]*value, error) {
	// Load the remaining entries.
	var entry [PersistedEntrySize]byte
	entries := make(map[crypto.Hash]*value)
	for index := int64(1); index < numEntries; index++ {
		_, err := io.ReadFull(r, entry[:])
		if err != nil {
			return nil, errors.AddContext(err, fmt.Sprintf("failed to read entry %v of %v", index, numEntries))
		}
		var pe persistedEntry
		err = pe.Unmarshal(entry[:])
		if err != nil {
			return nil, errors.AddContext(err, fmt.Sprintf("failed to parse entry %v of %v", index, numEntries))
		}
		if pe.Key == noKey {
			continue // ignore unused entries
		}
		// Set the type if it's not set.
		if pe.Type == 0 {
			pe.Type = persistedEntryType
		}
		// Add the entry to the store.
		v, err := pe.Value(index)
		if err != nil {
			return nil, errors.AddContext(err, fmt.Sprintf("failed to get key-value pair from entry %v of %v", index, numEntries))
		}
		entries[v.mapKey()] = v
		// Track it in the bitfield.
		err = b.Set(uint64(index) - 1)
		if err != nil {
			return nil, errors.AddContext(err, fmt.Sprintf("failed to mark entry %v of %v as used in bitfield", index, numEntries))
		}
	}
	return entries, nil
}

// newPersistedEntry turns a value type into a persistedEntry.
func newPersistedEntry(value *value) (persistedEntry, error) {
	if len(value.data) > modules.RegistryDataSize {
		build.Critical("newPersistedEntry: called with too much data")
		return persistedEntry{}, errors.New("value's data is too large")
	}
	cpk, err := newCompressedPublicKey(value.key)
	if err != nil {
		return persistedEntry{}, errors.AddContext(err, "newPersistedEntry: failed to compress key")
	}
	pe := persistedEntry{
		Key:       cpk,
		Signature: value.signature,
		Tweak:     value.tweak,

		DataLen:  uint8(len(value.data)),
		Expiry:   compressedBlockHeight(value.expiry),
		Revision: value.revision,
	}
	copy(pe.Data[:], value.data)
	return pe, nil
}

// Value converts a persistedEntry into a value type.
func (entry persistedEntry) Value(index int64) (*value, error) {
	if entry.DataLen > modules.RegistryDataSize {
		err := errors.New("Value: entry has a too big data len")
		build.Critical(err)
		return nil, err
	}
	spk, err := newUploPublicKey(entry.Key)
	if err != nil {
		return nil, errors.AddContext(err, "Value: failed to convert compressed key to UploPublicKey")
	}
	return &value{
		key:         spk,
		tweak:       entry.Tweak,
		expiry:      types.BlockHeight(entry.Expiry),
		data:        entry.Data[:entry.DataLen],
		revision:    entry.Revision,
		signature:   entry.Signature,
		staticIndex: index,
	}, nil
}

// Marshal marshals a persistedEntry.
func (entry persistedEntry) Marshal() ([]byte, error) {
	if entry.DataLen > modules.RegistryDataSize {
		build.Critical(errTooMuchData)
		return nil, errTooMuchData
	}
	b := make([]byte, PersistedEntrySize)
	b[0] = entry.Key.Algorithm
	copy(b[1:], entry.Key.Key[:])
	copy(b[33:], entry.Tweak[:])
	binary.LittleEndian.PutUint32(b[65:], uint32(entry.Expiry))
	binary.LittleEndian.PutUint64(b[69:], uint64(entry.Revision))
	copy(b[77:], entry.Signature[:])
	b[141] = byte(entry.DataLen)
	copy(b[142:], entry.Data[:])
	b[PersistedEntrySize-1] = entry.Type
	return b, nil
}

// Unmarshal unmarshals a persistedEntry.
func (entry *persistedEntry) Unmarshal(b []byte) error {
	if len(b) != PersistedEntrySize {
		build.Critical(errEntryWrongSize)
		return errEntryWrongSize
	}
	entry.Key.Algorithm = b[0]
	copy(entry.Key.Key[:], b[1:])
	copy(entry.Tweak[:], b[33:])
	entry.Expiry = compressedBlockHeight(binary.LittleEndian.Uint32(b[65:]))
	entry.Revision = binary.LittleEndian.Uint64(b[69:])
	copy(entry.Signature[:], b[77:])
	entry.DataLen = uint8(b[141])
	if int(entry.DataLen) > len(entry.Data) {
		return errTooMuchData
	}
	copy(entry.Data[:], b[142:PersistedEntrySize-1])
	entry.Type = b[PersistedEntrySize-1]
	return nil
}

// staticSaveEntry stores a value on disk atomically. If used is set, the entry
// will be marked as in use. Otherwise a sentinel value will be persisted.
// NOTE: v.mu is expected to be acquired.
func (r *Registry) staticSaveEntry(v *value, used bool) error {
	var entry persistedEntry
	var err error
	if used {
		entry, err = newPersistedEntry(v)
	}
	if err != nil {
		return errors.AddContext(err, "Save: failed to get persistedEntry from key-value pair")
	}
	b, err := entry.Marshal()
	if err != nil {
		return errors.AddContext(err, "Save: failed to marshal persistedEntry")
	}
	_, err = r.staticFile.WriteAt(b, v.staticIndex*PersistedEntrySize)
	if err != nil {
		return errors.AddContext(err, "failed to save entry")
	}
	return nil
}
