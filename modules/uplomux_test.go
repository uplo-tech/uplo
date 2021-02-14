package modules

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
)

// TestUploMuxCompat verifies the UploMux is initialized in compatibility mode
// when the host's persistence metadata version is v1.4.2
func TestUploMuxCompat(t *testing.T) {
	// ensure the host's persistence file does not exist
	uplodataDir := filepath.Join(os.TempDir(), t.Name())
	uploMuxDir := filepath.Join(uplodataDir, UploMuxDir)
	persistPath := filepath.Join(uplodataDir, HostDir, HostSettingsFile)
	os.Remove(persistPath)

	// create a new uplomux, seeing as there won't be a host persistence file, it
	// will act as if this is a fresh new node and create a new key pair
	mux, err := NewUploMux(uploMuxDir, uplodataDir, "localhost:0", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	expectedPK := mux.PublicKey()
	expectedSK := mux.PrivateKey()
	mux.Close()

	// re-open the mux and verify it uses the same keys
	mux, err = NewUploMux(uploMuxDir, uplodataDir, "localhost:0", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	actualPK := mux.PublicKey()
	actualSK := mux.PrivateKey()
	if !bytes.Equal(actualPK[:], expectedPK[:]) {
		t.Log(actualPK)
		t.Log(expectedPK)
		t.Fatal("UploMux's public key was different after reloading the mux")
	}
	if !bytes.Equal(actualSK[:], expectedSK[:]) {
		t.Log(actualSK)
		t.Log(expectedSK)
		t.Fatal("UploMux's private key was different after reloading the mux")
	}
	mux.Close()

	// prepare a host's persistence file with v1.4.2 and verify the mux is now
	// initialised using the host's key pair

	// create the host directory if it doesn't exist.
	err = os.MkdirAll(filepath.Join(uplodataDir, HostDir), 0700)
	if err != nil {
		t.Fatal(err)
	}

	sk, pk := crypto.GenerateKeyPair()
	spk := types.UploPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	}
	persistence := struct {
		PublicKey types.UploPublicKey `json:"publickey"`
		SecretKey crypto.SecretKey   `json:"secretkey"`
	}{
		PublicKey: spk,
		SecretKey: sk,
	}
	err = persist.SaveJSON(Hostv120PersistMetadata, persistence, persistPath)
	if err != nil {
		t.Fatal(err)
	}

	// create a new uplomux
	mux, err = NewUploMux(uploMuxDir, uplodataDir, "localhost:0", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	actualPK = mux.PublicKey()
	actualSK = mux.PrivateKey()
	if !bytes.Equal(actualPK[:], spk.Key) {
		t.Log(actualPK)
		t.Log(spk.Key)
		t.Fatal("UploMux's public key was not equal to the host's pubkey")
	}
	if !bytes.Equal(actualSK[:], sk[:]) {
		t.Log(mux.PrivateKey())
		t.Log(spk.Key)
		t.Fatal("UploMux's public key was not equal to the host's pubkey")
	}
}

// TestUploMuxAbsolutePath verifies we can not create the UploMux using a relative
// path for neither the uplomux dir nor the uplo data dir.
func TestUploMuxAbsolutePath(t *testing.T) {
	t.Parallel()

	assertRecover := func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected a panic when a relative path is passed to the UploMux")
		}
	}

	absPath := os.TempDir()
	for _, relPath := range []string{"", ".", ".."} {
		func() {
			defer assertRecover()
			NewUploMux(absPath, relPath, "localhost:0", "localhost:0")
		}()
		func() {
			defer assertRecover()
			NewUploMux(relPath, absPath, "localhost:0", "localhost:0")
		}()
	}
}
