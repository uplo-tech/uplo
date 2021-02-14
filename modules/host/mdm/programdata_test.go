package mdm

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// TestNewProgramData tests starting and stopping a ProgramData object.
func TestNewProgramData(t *testing.T) {
	buf := bytes.NewReader(fastrand.Bytes(10))
	pd := openProgramData(buf, uint64(buf.Len()))
	defer func() {
		if err := pd.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}

// TestHash will test a number of random calls to Hash which should be
// successful.
func TestHash(t *testing.T) {
	data := fastrand.Bytes(1000)
	buf := bytes.NewReader(data)
	pd := openProgramData(buf, uint64(len(data)))
	for i := 0; i < 1000; i++ {
		offset := fastrand.Intn(len(data) - crypto.HashSize + 1)
		h, err := pd.Hash(uint64(offset))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(h[:], data[offset:][:crypto.HashSize]) {
			t.Fatalf("hash should be %v but was %v", data[offset:][:crypto.HashSize], h[:])
		}
	}
	defer func() {
		if err := pd.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}

// TestUint64 will test a number of random calls to Uint64 which should be
// successful.
func TestUint64(t *testing.T) {
	data := fastrand.Bytes(1000)
	buf := bytes.NewReader(data)
	pd := openProgramData(buf, uint64(len(data)))
	for i := 0; i < 1000; i++ {
		offset := fastrand.Intn(len(data) - 8 + 1)
		n, err := pd.Uint64(uint64(offset))
		if err != nil {
			t.Fatal(err)
		}
		if expected := binary.LittleEndian.Uint64(data[offset:][:8]); expected != n {
			t.Fatalf("uint64 should be %v but was %v", expected, n)
		}
	}
	defer func() {
		if err := pd.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}

// TestOutOfBounds tests the out-of-bounds check.
func TestOutOfBounds(t *testing.T) {
	buf := bytes.NewReader(fastrand.Bytes(8))
	pd := openProgramData(buf, 7)
	_, err := pd.managedBytes(0, 8)
	if err == nil {
		t.Fatal("managedBytes should fail")
	}
	defer func() {
		if err := pd.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}

// TestEOFWhileReading tests that an error returned by the reader is correctly
// returned.
func TestEOFWhileReading(t *testing.T) {
	r := bytes.NewReader(fastrand.Bytes(7))
	pd := openProgramData(r, 8)
	cont := make(chan struct{})
	go func() {
		<-cont
		defer func() {
			if err := pd.Close(); err != nil {
				t.Fatal(err)
			}
		}()
	}()
	_, err := pd.Uint64(0)
	if !errors.Contains(err, io.EOF) {
		t.Errorf("error was supposed to be %v but was %v", io.EOF, err)
	}
	close(cont)
}
