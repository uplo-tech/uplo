package mdm

import (
	"encoding/binary"
	"fmt"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// instructionReadRegistry defines an instruction to read an entry from the
// registry.
type instructionReadRegistry struct {
	commonInstruction

	pubKeyOffset uint64
	pubKeyLength uint64
	tweakOffset  uint64
}

// staticDecodeReadRegistryInstruction creates a new 'ReadRegistry' instruction
// from the provided generic instruction.
func (p *program) staticDecodeReadRegistryInstruction(instruction modules.Instruction) (instruction, error) {
	// Check specifier.
	if instruction.Specifier != modules.SpecifierReadRegistry {
		return nil, fmt.Errorf("expected specifier %v but got %v",
			modules.SpecifierReadRegistry, instruction.Specifier)
	}
	// Check args.
	if len(instruction.Args) != modules.RPCIReadRegistryLen {
		return nil, fmt.Errorf("expected instruction to have len %v but was %v",
			modules.RPCIReadRegistryLen, len(instruction.Args))
	}
	// Read args.
	pubKeyOffset := binary.LittleEndian.Uint64(instruction.Args[:8])
	pubKeyLength := binary.LittleEndian.Uint64(instruction.Args[8:16])
	tweakOffset := binary.LittleEndian.Uint64(instruction.Args[16:24])
	return &instructionReadRegistry{
		commonInstruction: commonInstruction{
			staticData:  p.staticData,
			staticState: p.staticProgramState,
		},
		pubKeyOffset: pubKeyOffset,
		pubKeyLength: pubKeyLength,
		tweakOffset:  tweakOffset,
	}, nil
}

// Execute executes the 'ReadRegistry' instruction.
func (i *instructionReadRegistry) Execute(prevOutput output) (output, types.Currency) {
	// Fetch the args.
	pubKey, err := i.staticData.UploPublicKey(i.pubKeyOffset, i.pubKeyLength)
	if err != nil {
		return errOutput(err), types.ZeroCurrency
	}
	tweak, err := i.staticData.Hash(i.tweakOffset)
	if err != nil {
		return errOutput(err), types.ZeroCurrency
	}

	// Prepare the output. An empty output.Output means the data wasn't found.
	out := output{
		NewSize:       prevOutput.NewSize,
		NewMerkleRoot: prevOutput.NewMerkleRoot,
		Output:        nil,
	}

	// Get the value. If this fails we are done.
	rv, found := i.staticState.host.RegistryGet(pubKey, tweak)
	if !found {
		_, refund := modules.MDMReadRegistryCost(i.staticState.priceTable)
		return out, refund
	}

	// Return the signature followed by the data.
	rev := make([]byte, 8)
	binary.LittleEndian.PutUint64(rev, rv.Revision)
	out.Output = append(rv.Signature[:], append(rev, rv.Data...)...)
	return out, types.ZeroCurrency
}

// Registry reads can be batched, because they are both tiny, and low latency.
// Typical case is an in-memory lookup, worst case is a small, single on-disk
// read.
func (i *instructionReadRegistry) Batch() bool {
	return true
}

// Collateral returns the collateral the host has to put up for this
// instruction.
func (i *instructionReadRegistry) Collateral() types.Currency {
	return modules.MDMReadRegistryCollateral()
}

// Cost returns the Cost of this `ReadRegistry` instruction.
func (i *instructionReadRegistry) Cost() (executionCost, refund types.Currency, err error) {
	executionCost, refund = modules.MDMReadRegistryCost(i.staticState.priceTable)
	return
}

// Memory returns the memory allocated by the 'ReadRegistry' instruction beyond the
// lifetime of the instruction.
func (i *instructionReadRegistry) Memory() uint64 {
	return modules.MDMReadRegistryMemory()
}

// Time returns the execution time of an 'ReadRegistry' instruction.
func (i *instructionReadRegistry) Time() (uint64, error) {
	return modules.MDMTimeReadRegistry, nil
}
