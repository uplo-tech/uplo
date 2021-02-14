package renter

import (
	"io"
	"strings"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"
)

// TestSkyfileFanout probes the fanout encoding.
func TestSkyfileFanout(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a renter for the tests
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	t.Run("Panics", func(t *testing.T) { testSkyfileEncodeFanout_Panic(t, rt) })
	t.Run("Reader", func(t *testing.T) { testSkyfileEncodeFanout_Reader(t, rt) })
}

// testSkyfileEncodeFanout_Panic probes the panic conditions for generating the
// fanout
func testSkyfileEncodeFanout_Panic(t *testing.T, rt *renterTester) {
	// Create a file for the renter with erasure coding of 1-of-N and a PlainText
	// cipher type.
	uploPath, rsc := testingFileParamsCustom(1, 2)
	file, err := rt.renter.createRenterTestFileWithParams(uploPath, rsc, crypto.TypePlain)
	if err != nil {
		t.Fatal(err)
	}
	testPanic(t, file, nil)

	// Create a file for the renter with erasure coding of N-of-M and a non
	// PlainText cipher type.
	uploPath, rsc = testingFileParamsCustom(2, 3)
	file, err = rt.renter.createRenterTestFileWithParams(uploPath, rsc, crypto.TypeDefaultRenter)
	if err != nil {
		t.Fatal(err)
	}
	testPanic(t, file, nil)
}

// testPanic executes the function and recovers from the expected panic.
func testPanic(t *testing.T, fileNode *filesystem.FileNode, reader io.Reader) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected build critical for empty hash in fanout")
		}
	}()
	skyfileEncodeFanout(fileNode, reader)
}

// testSkyfileEncodeFanout_Reader probes generating the fanout from a reader
func testSkyfileEncodeFanout_Reader(t *testing.T, rt *renterTester) {
	// Create a file with N-of-M erasure coding and a non PlainText cipher type
	uploPath, rsc := testingFileParamsCustom(2, 3)
	file, err := rt.renter.createRenterTestFileWithParams(uploPath, rsc, crypto.TypeDefaultRenter)
	if err != nil {
		t.Fatal(err)
	}

	// Create a mock reader to the file on disk
	reader := strings.NewReader("this is fine")

	// Even though the file is not uploaded, we should be able to create the
	// fanout from the file on disk.
	//
	// Since we are using test data we don't care about the final result of the
	// fanout, we just are testing that the panics aren't triggered.
	_, err = skyfileEncodeFanout(file, reader)
	if err != nil {
		t.Fatal(err)
	}

	// Create a file with 1-of-N erasure coding and a non PlainText cipher type
	uploPath, rsc = testingFileParamsCustom(1, 3)
	file, err = rt.renter.createRenterTestFileWithParams(uploPath, rsc, crypto.TypeDefaultRenter)
	if err != nil {
		t.Fatal(err)
	}

	// Create a mock reader to the file on disk
	reader = strings.NewReader("still fine")

	// Even though the file is not uploaded, we should be able to create the
	// fanout from the file on disk.
	//
	// Since we are using test data we don't care about the final result of the
	// fanout, we just are testing that the panics aren't triggered.
	_, err = skyfileEncodeFanout(file, reader)
	if err != nil {
		t.Fatal(err)
	}
}
