package mdm

import (
	"encoding/binary"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/fastrand"
)

// TestInstructionReadRegistry tests the ReadRegistry instruction.
func TestInstructionReadRegistry(t *testing.T) {
	host := newTestHost()
	mdm := New(host)
	defer mdm.Stop()

	// Add a registry value for a given random key/tweak pair.
	sk, pk := crypto.GenerateKeyPair()
	var tweak crypto.Hash
	fastrand.Read(tweak[:])
	data := fastrand.Bytes(modules.RegistryDataSize)
	rev := fastrand.Uint64n(1000)
	spk := types.UploPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	}
	rv := modules.NewRegistryValue(tweak, data, rev).Sign(sk)
	_, err := host.RegistryUpdate(rv, spk, types.BlockHeight(fastrand.Uint64n(1000)))
	if err != nil {
		t.Fatal(err)
	}

	so := host.newTestStorageObligation(true)
	pt := newTestPriceTable()
	tb := newTestProgramBuilder(pt, 0)
	tb.AddReadRegistryInstruction(spk, tweak, false)

	// Execute it.
	outputs, err := mdm.ExecuteProgramWithBuilder(tb, so, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	// Assert output.
	output := outputs[0]
	revBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(revBytes, rev)
	expectedOutput := append(rv.Signature[:], append(revBytes, rv.Data...)...)
	err = output.assert(0, crypto.Hash{}, []crypto.Hash{}, expectedOutput, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the signature.
	var sig2 crypto.Signature
	copy(sig2[:], output.Output[:crypto.SignatureSize])
	rev2 := binary.LittleEndian.Uint64(output.Output[crypto.SignatureSize:])
	data2 := output.Output[crypto.SignatureSize+8:]
	rv2 := modules.NewSignedRegistryValue(tweak, data2, rev2, sig2)
	if rv2.Verify(pk) != nil {
		t.Fatal("verification failed", err)
	}
}

// TestInstructionReadRegistryNotFound tests the ReadRegistry instruction for
// when an entry isn't found.
func TestInstructionReadRegistryNotFound(t *testing.T) {
	host := newTestHost()
	mdm := New(host)
	defer mdm.Stop()

	// Add a registry value for a given random key/tweak pair.
	_, pk := crypto.GenerateKeyPair()
	spk := types.UploPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	}

	so := host.newTestStorageObligation(true)
	pt := newTestPriceTable()
	tb := newTestProgramBuilder(pt, 0)
	refund := tb.AddReadRegistryInstruction(spk, crypto.Hash{}, true)

	// Execute it.
	outputs, remainingBudget, err := mdm.ExecuteProgramWithBuilderCustomBudget(tb, so, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if outputs[0].Error != nil {
		t.Fatal("error returned", outputs[0].Error)
	}
	if len(outputs[0].Output) != 0 {
		t.Fatal("expected empty output")
	}
	if !remainingBudget.Remaining().Equals(refund) {
		t.Fatal("remaining budget should equal refund", remainingBudget.Remaining().HumanString(), refund.HumanString())
	}
}
