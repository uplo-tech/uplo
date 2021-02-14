package uplofile

import (
	"testing"

	"github.com/uplo-tech/writeaheadlog"
)

// TestMarshalChunkSize checks marshaledChunkSize against the expected values.
// This guarantees that we can't accidentally change any constants without
// noticing.
func TestMarshalChunkSize(t *testing.T) {
	chunkOverhead := 16 + 2 + 1
	pieceSize := 4 + 4 + 32
	for i := 0; i < 100; i++ {
		if marshaledChunkSize(i) != int64(chunkOverhead+i*pieceSize) {
			t.Fatalf("Expected chunkSize %v but was %v",
				chunkOverhead+i*pieceSize, marshaledChunkSize(i))
		}
	}
}

// TestIsUploFileUpdate tests the IsUploFileUpdate method.
func TestIsUploFileUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newTestFile()
	insertUpdate := sf.createInsertUpdate(0, []byte{})
	deleteUpdate := sf.createDeleteUpdate()
	randomUpdate := writeaheadlog.Update{}

	if !IsUploFileUpdate(insertUpdate) {
		t.Error("insertUpdate should be a UploFileUpdate but wasn't")
	}
	if !IsUploFileUpdate(deleteUpdate) {
		t.Error("deleteUpdate should be a UploFileUpdate but wasn't")
	}
	if IsUploFileUpdate(randomUpdate) {
		t.Error("randomUpdate shouldn't be a UploFileUpdate but was one")
	}
}
