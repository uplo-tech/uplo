package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"
	"unsafe"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
)

// MarshalUplo implements the encoding.UploMarshaler interface.
func (b Block) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	_, _ = e.Write(b.ParentID[:])
	_, _ = e.Write(b.Nonce[:])
	_ = e.WriteUint64(uint64(b.Timestamp))
	_ = e.WriteInt(len(b.MinerPayouts))
	for i := range b.MinerPayouts {
		b.MinerPayouts[i].MarshalUplo(e)
	}
	_ = e.WriteInt(len(b.Transactions))
	for i := range b.Transactions {
		if err := b.Transactions[i].MarshalUplo(e); err != nil {
			return err
		}
	}
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (b *Block) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, int(BlockSizeLimit*3))
	d.ReadFull(b.ParentID[:])
	d.ReadFull(b.Nonce[:])
	b.Timestamp = Timestamp(d.NextUint64())
	// MinerPayouts
	b.MinerPayouts = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range b.MinerPayouts {
		b.MinerPayouts[i].UnmarshalUplo(d)
	}
	// Transactions
	b.Transactions = make([]Transaction, d.NextPrefix(unsafe.Sizeof(Transaction{})))
	for i := range b.Transactions {
		b.Transactions[i].UnmarshalUplo(d)
	}
	return d.Err()
}

// MarshalJSON marshales a block id as a hex string.
func (bid BlockID) MarshalJSON() ([]byte, error) {
	return json.Marshal(bid.String())
}

// String prints the block id in hex.
func (bid BlockID) String() string {
	return fmt.Sprintf("%x", bid[:])
}

// LoadString loads a BlockID from a string
func (bid *BlockID) LoadString(str string) error {
	return (*crypto.Hash)(bid).LoadString(str)
}

// UnmarshalJSON decodes the json hex string of the block id.
func (bid *BlockID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(bid).UnmarshalJSON(b)
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (cf CoveredFields) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.WriteBool(cf.WholeTransaction)
	fields := [][]uint64{
		cf.UplocoinInputs,
		cf.UplocoinOutputs,
		cf.FileContracts,
		cf.FileContractRevisions,
		cf.StorageProofs,
		cf.UplofundInputs,
		cf.UplofundOutputs,
		cf.MinerFees,
		cf.ArbitraryData,
		cf.TransactionSignatures,
	}
	for _, f := range fields {
		e.WriteInt(len(f))
		for _, u := range f {
			e.WriteUint64(u)
		}
	}
	return e.Err()
}

// MarshalUploSize returns the encoded size of cf.
func (cf CoveredFields) MarshalUploSize() (size int) {
	size++ // WholeTransaction
	size += 8 + len(cf.UplocoinInputs)*8
	size += 8 + len(cf.UplocoinOutputs)*8
	size += 8 + len(cf.FileContracts)*8
	size += 8 + len(cf.FileContractRevisions)*8
	size += 8 + len(cf.StorageProofs)*8
	size += 8 + len(cf.UplofundInputs)*8
	size += 8 + len(cf.UplofundOutputs)*8
	size += 8 + len(cf.MinerFees)*8
	size += 8 + len(cf.ArbitraryData)*8
	size += 8 + len(cf.TransactionSignatures)*8
	return
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (cf *CoveredFields) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	buf := make([]byte, 1)
	d.ReadFull(buf)
	cf.WholeTransaction = (buf[0] == 1)
	fields := []*[]uint64{
		&cf.UplocoinInputs,
		&cf.UplocoinOutputs,
		&cf.FileContracts,
		&cf.FileContractRevisions,
		&cf.StorageProofs,
		&cf.UplofundInputs,
		&cf.UplofundOutputs,
		&cf.MinerFees,
		&cf.ArbitraryData,
		&cf.TransactionSignatures,
	}
	for i := range fields {
		f := make([]uint64, d.NextPrefix(unsafe.Sizeof(uint64(0))))
		for i := range f {
			f[i] = d.NextUint64()
		}
		*fields[i] = f
	}
	return d.Err()
}

// MarshalJSON implements the json.Marshaler interface.
func (c Currency) MarshalJSON() ([]byte, error) {
	// Must enclosed the value in quotes; otherwise JS will convert it to a
	// double and lose precision.
	return []byte(`"` + c.String() + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. An error is
// returned if a negative number is provided.
func (c *Currency) UnmarshalJSON(b []byte) error {
	// UnmarshalJSON does not expect quotes
	b = bytes.Trim(b, `"`)
	err := c.i.UnmarshalJSON(b)
	if err != nil {
		return err
	}
	if c.i.Sign() < 0 {
		c.i = *big.NewInt(0)
		return ErrNegativeCurrency
	}
	return nil
}

// MarshalUplo implements the encoding.UploMarshaler interface. It writes the
// byte-slice representation of the Currency's internal big.Int to w. Note
// that as the bytes of the big.Int correspond to the absolute value of the
// integer, there is no way to marshal a negative Currency.
func (c Currency) MarshalUplo(w io.Writer) error {
	// from math/big/arith.go
	const (
		_m    = ^big.Word(0)
		_logS = _m>>8&1 + _m>>16&1 + _m>>32&1
		_S    = 1 << _logS // number of bytes per big.Word
	)

	// get raw bits and seek to first zero byte
	bits := c.i.Bits()
	var i int
	for i = len(bits)*_S - 1; i >= 0; i-- {
		if bits[i/_S]>>(uint(i%_S)*8) != 0 {
			break
		}
	}

	// write length prefix
	e := encoding.NewEncoder(w)
	e.WriteInt(i + 1)

	// write bytes
	for ; i >= 0; i-- {
		e.WriteByte(byte(bits[i/_S] >> (uint(i%_S) * 8)))
	}
	return e.Err()
}

// MarshalUploSize returns the encoded size of c.
func (c Currency) MarshalUploSize() int {
	// from math/big/arith.go
	const (
		_m    = ^big.Word(0)
		_logS = _m>>8&1 + _m>>16&1 + _m>>32&1
		_S    = 1 << _logS // number of bytes per big.Word
	)

	// start with the number of Words * number of bytes per Word, then
	// subtract trailing bytes that are 0
	bits := c.i.Bits()
	size := len(bits) * _S
zeros:
	for i := len(bits) - 1; i >= 0; i-- {
		for j := _S - 1; j >= 0; j-- {
			if (bits[i] >> uintptr(j*8)) != 0 {
				break zeros
			}
			size--
		}
	}
	return 8 + size // account for length prefix
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (c *Currency) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	var dec Currency
	dec.i.SetBytes(d.ReadPrefixedBytes())
	*c = dec
	return d.Err()
}

// HumanString prints the Currency using human readable units. The unit used
// will be the largest unit that results in a value greater than 1. The value is
// rounded to 4 significant digits.
func (c Currency) HumanString() string {
	pico := UplocoinPrecision.Div64(1e12)
	if c.Cmp(pico) < 0 {
		return c.String() + " H"
	}

	// iterate until we find a unit greater than c
	mag := pico
	unit := ""
	for _, unit = range []string{"pS", "nS", "uS", "mS", "UC", "KS", "MS", "GS", "TS"} {
		if c.Cmp(mag.Mul64(1e3)) < 0 {
			break
		} else if unit != "TS" {
			// don't want to perform this multiply on the last iter; that
			// would give us 1.235 TS instead of 1235 TS
			mag = mag.Mul64(1e3)
		}
	}

	num := new(big.Rat).SetInt(c.Big())
	denom := new(big.Rat).SetInt(mag.Big())
	res, _ := new(big.Rat).Mul(num, denom.Inv(denom)).Float64()

	return fmt.Sprintf("%.4g %s", res, unit)
}

// String implements the fmt.Stringer interface.
func (c Currency) String() string {
	return c.i.String()
}

// Scan implements the fmt.Scanner interface, allowing Currency values to be
// scanned from text.
func (c *Currency) Scan(s fmt.ScanState, ch rune) error {
	var dec Currency
	err := dec.i.Scan(s, ch)
	if err != nil {
		return err
	}
	if dec.i.Sign() < 0 {
		return ErrNegativeCurrency
	}
	*c = dec
	return nil
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (fc FileContract) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.WriteUint64(fc.FileSize)
	e.Write(fc.FileMerkleRoot[:])
	e.WriteUint64(uint64(fc.WindowStart))
	e.WriteUint64(uint64(fc.WindowEnd))
	fc.Payout.MarshalUplo(e)
	e.WriteInt(len(fc.ValidProofOutputs))
	for _, sco := range fc.ValidProofOutputs {
		sco.MarshalUplo(e)
	}
	e.WriteInt(len(fc.MissedProofOutputs))
	for _, sco := range fc.MissedProofOutputs {
		sco.MarshalUplo(e)
	}
	e.Write(fc.UnlockHash[:])
	e.WriteUint64(fc.RevisionNumber)
	return e.Err()
}

// MarshalUploSize returns the encoded size of fc.
func (fc FileContract) MarshalUploSize() (size int) {
	size += 8 // FileSize
	size += len(fc.FileMerkleRoot)
	size += 8 + 8 // WindowStart + WindowEnd
	size += fc.Payout.MarshalUploSize()
	size += 8
	for _, sco := range fc.ValidProofOutputs {
		size += sco.Value.MarshalUploSize()
		size += len(sco.UnlockHash)
	}
	size += 8
	for _, sco := range fc.MissedProofOutputs {
		size += sco.Value.MarshalUploSize()
		size += len(sco.UnlockHash)
	}
	size += len(fc.UnlockHash)
	size += 8 // RevisionNumber
	return
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (fc *FileContract) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	fc.FileSize = d.NextUint64()
	d.ReadFull(fc.FileMerkleRoot[:])
	fc.WindowStart = BlockHeight(d.NextUint64())
	fc.WindowEnd = BlockHeight(d.NextUint64())
	fc.Payout.UnmarshalUplo(d)
	fc.ValidProofOutputs = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range fc.ValidProofOutputs {
		fc.ValidProofOutputs[i].UnmarshalUplo(d)
	}
	fc.MissedProofOutputs = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range fc.MissedProofOutputs {
		fc.MissedProofOutputs[i].UnmarshalUplo(d)
	}
	d.ReadFull(fc.UnlockHash[:])
	fc.RevisionNumber = d.NextUint64()
	return d.Err()
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (fcr FileContractRevision) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(fcr.ParentID[:])
	fcr.UnlockConditions.MarshalUplo(e)
	e.WriteUint64(fcr.NewRevisionNumber)
	e.WriteUint64(fcr.NewFileSize)
	e.Write(fcr.NewFileMerkleRoot[:])
	e.WriteUint64(uint64(fcr.NewWindowStart))
	e.WriteUint64(uint64(fcr.NewWindowEnd))
	e.WriteInt(len(fcr.NewValidProofOutputs))
	for _, sco := range fcr.NewValidProofOutputs {
		sco.MarshalUplo(e)
	}
	e.WriteInt(len(fcr.NewMissedProofOutputs))
	for _, sco := range fcr.NewMissedProofOutputs {
		sco.MarshalUplo(e)
	}
	e.Write(fcr.NewUnlockHash[:])
	return e.Err()
}

// MarshalUploSize returns the encoded size of fcr.
func (fcr FileContractRevision) MarshalUploSize() (size int) {
	size += len(fcr.ParentID)
	size += fcr.UnlockConditions.MarshalUploSize()
	size += 8 // NewRevisionNumber
	size += 8 // NewFileSize
	size += len(fcr.NewFileMerkleRoot)
	size += 8 + 8 // NewWindowStart + NewWindowEnd
	size += 8
	for _, sco := range fcr.NewValidProofOutputs {
		size += sco.Value.MarshalUploSize()
		size += len(sco.UnlockHash)
	}
	size += 8
	for _, sco := range fcr.NewMissedProofOutputs {
		size += sco.Value.MarshalUploSize()
		size += len(sco.UnlockHash)
	}
	size += len(fcr.NewUnlockHash)
	return
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (fcr *FileContractRevision) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(fcr.ParentID[:])
	fcr.UnlockConditions.UnmarshalUplo(d)
	fcr.NewRevisionNumber = d.NextUint64()
	fcr.NewFileSize = d.NextUint64()
	d.ReadFull(fcr.NewFileMerkleRoot[:])
	fcr.NewWindowStart = BlockHeight(d.NextUint64())
	fcr.NewWindowEnd = BlockHeight(d.NextUint64())
	fcr.NewValidProofOutputs = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range fcr.NewValidProofOutputs {
		fcr.NewValidProofOutputs[i].UnmarshalUplo(d)
	}
	fcr.NewMissedProofOutputs = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range fcr.NewMissedProofOutputs {
		fcr.NewMissedProofOutputs[i].UnmarshalUplo(d)
	}
	d.ReadFull(fcr.NewUnlockHash[:])
	return d.Err()
}

// LoadString loads a FileContractID from a string
func (fcid *FileContractID) LoadString(str string) error {
	return (*crypto.Hash)(fcid).LoadString(str)
}

// MarshalJSON marshals an id as a hex string.
func (fcid FileContractID) MarshalJSON() ([]byte, error) {
	return json.Marshal(fcid.String())
}

// String prints the id in hex.
func (fcid FileContractID) String() string {
	return fmt.Sprintf("%x", fcid[:])
}

// UnmarshalJSON decodes the json hex string of the id.
func (fcid *FileContractID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(fcid).UnmarshalJSON(b)
}

// MarshalJSON marshals an id as a hex string.
func (oid OutputID) MarshalJSON() ([]byte, error) {
	return json.Marshal(oid.String())
}

// String prints the id in hex.
func (oid OutputID) String() string {
	return fmt.Sprintf("%x", oid[:])
}

// UnmarshalJSON decodes the json hex string of the id.
func (oid *OutputID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(oid).UnmarshalJSON(b)
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (sci UplocoinInput) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(sci.ParentID[:])
	sci.UnlockConditions.MarshalUplo(e)
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (sci *UplocoinInput) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(sci.ParentID[:])
	sci.UnlockConditions.UnmarshalUplo(d)
	return d.Err()
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (sco UplocoinOutput) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	sco.Value.MarshalUplo(e)
	e.Write(sco.UnlockHash[:])
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (sco *UplocoinOutput) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	sco.Value.UnmarshalUplo(d)
	d.ReadFull(sco.UnlockHash[:])
	return d.Err()
}

// MarshalJSON marshals an id as a hex string.
func (scoid UplocoinOutputID) MarshalJSON() ([]byte, error) {
	return json.Marshal(scoid.String())
}

// String prints the id in hex.
func (scoid UplocoinOutputID) String() string {
	return fmt.Sprintf("%x", scoid[:])
}

// UnmarshalJSON decodes the json hex string of the id.
func (scoid *UplocoinOutputID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(scoid).UnmarshalJSON(b)
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (sfi UplofundInput) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(sfi.ParentID[:])
	sfi.UnlockConditions.MarshalUplo(e)
	e.Write(sfi.ClaimUnlockHash[:])
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (sfi *UplofundInput) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(sfi.ParentID[:])
	sfi.UnlockConditions.UnmarshalUplo(d)
	d.ReadFull(sfi.ClaimUnlockHash[:])
	return d.Err()
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (sfo UplofundOutput) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	sfo.Value.MarshalUplo(e)
	e.Write(sfo.UnlockHash[:])
	sfo.ClaimStart.MarshalUplo(e)
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (sfo *UplofundOutput) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	sfo.Value.UnmarshalUplo(d)
	d.ReadFull(sfo.UnlockHash[:])
	sfo.ClaimStart.UnmarshalUplo(d)
	return d.Err()
}

// MarshalJSON marshals an id as a hex string.
func (sfoid UplofundOutputID) MarshalJSON() ([]byte, error) {
	return json.Marshal(sfoid.String())
}

// String prints the id in hex.
func (sfoid UplofundOutputID) String() string {
	return fmt.Sprintf("%x", sfoid[:])
}

// UnmarshalJSON decodes the json hex string of the id.
func (sfoid *UplofundOutputID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(sfoid).UnmarshalJSON(b)
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (spk UploPublicKey) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(spk.Algorithm[:])
	e.WritePrefixedBytes(spk.Key)
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (spk *UploPublicKey) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(spk.Algorithm[:])
	spk.Key = d.ReadPrefixedBytes()
	return d.Err()
}

// LoadString is the inverse of UploPublicKey.String().
func (spk *UploPublicKey) LoadString(s string) error {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return errors.New("LoadString failed due to missing specifier")
	}
	var err error
	spk.Key, err = hex.DecodeString(parts[1])
	if err != nil {
		spk.Key = nil
		return errors.AddContext(err, "LoadString failed due to invalid hex encoding")
	}
	specifierBytes := parts[0]
	if len(spk.Algorithm) < len(specifierBytes) {
		return errors.New("LoadString failed due to specifier having invalid length")
	}
	copy(spk.Algorithm[:], specifierBytes)
	return nil
}

// String defines how to print a UploPublicKey - hex is used to keep things
// compact during logging. The key type prefix and lack of a checksum help to
// separate it from a uplo address.
func (spk UploPublicKey) String() string {
	if spk.Algorithm == SignatureEd25519 {
		buf := make([]byte, 72)
		copy(buf[:8], "ed25519:")
		hex.Encode(buf[8:], spk.Key)
		return string(buf)
	}
	return spk.Algorithm.String() + ":" + hex.EncodeToString(spk.Key)
}

// ShortString is a convenience function that returns a representation of the
// public key that can be used for logging or debugging. It returns the
// first 16 bytes of the actual key hex encoded.
//
// NOTE: this function should only be used for testing and/or debugging
// purposes, do not use this key representation as key in a map for instance.
func (spk UploPublicKey) ShortString() string {
	// if the key is empty, return the empty string
	if spk.Key == nil {
		return ""
	}
	return hex.EncodeToString(spk.Key[:16])
}

// UnmarshalJSON unmarshals a UploPublicKey as JSON.
func (spk *UploPublicKey) UnmarshalJSON(b []byte) error {
	spk.LoadString(string(bytes.Trim(b, `"`)))
	if spk.Key == nil {
		// fallback to old (base64) encoding
		var oldSPK struct {
			Algorithm Specifier
			Key       []byte
		}
		if err := json.Unmarshal(b, &oldSPK); err != nil {
			return err
		}
		spk.Algorithm, spk.Key = oldSPK.Algorithm, oldSPK.Key
	}
	return nil
}

// MarshalJSON marshals a specifier as a string.
func (s Specifier) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// String returns the specifier as a string, trimming any trailing zeros.
func (s Specifier) String() string {
	return string(bytes.TrimRight(s[:], RuneToString(0)))
}

// UnmarshalJSON decodes the json string of the specifier.
func (s *Specifier) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	copy(s[:], str)
	return nil
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (sp *StorageProof) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(sp.ParentID[:])
	e.Write(sp.Segment[:])
	e.WriteInt(len(sp.HashSet))
	for i := range sp.HashSet {
		e.Write(sp.HashSet[i][:])
	}
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (sp *StorageProof) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(sp.ParentID[:])
	d.ReadFull(sp.Segment[:])
	sp.HashSet = make([]crypto.Hash, d.NextPrefix(unsafe.Sizeof(crypto.Hash{})))
	for i := range sp.HashSet {
		d.ReadFull(sp.HashSet[i][:])
	}
	return d.Err()
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (t Transaction) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	t.marshalUploNoSignatures(e)
	e.WriteInt(len((t.TransactionSignatures)))
	for i := range t.TransactionSignatures {
		t.TransactionSignatures[i].MarshalUplo(e)
	}
	return e.Err()
}

// marshalUploNoSignatures is a helper function for calculating certain hashes
// that do not include the transaction's signatures.
func (t Transaction) marshalUploNoSignatures(w io.Writer) {
	e := encoding.NewEncoder(w)
	e.WriteInt(len((t.UplocoinInputs)))
	for i := range t.UplocoinInputs {
		t.UplocoinInputs[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.UplocoinOutputs)))
	for i := range t.UplocoinOutputs {
		t.UplocoinOutputs[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.FileContracts)))
	for i := range t.FileContracts {
		t.FileContracts[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.FileContractRevisions)))
	for i := range t.FileContractRevisions {
		t.FileContractRevisions[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.StorageProofs)))
	for i := range t.StorageProofs {
		t.StorageProofs[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.UplofundInputs)))
	for i := range t.UplofundInputs {
		t.UplofundInputs[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.UplofundOutputs)))
	for i := range t.UplofundOutputs {
		t.UplofundOutputs[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.MinerFees)))
	for i := range t.MinerFees {
		t.MinerFees[i].MarshalUplo(e)
	}
	e.WriteInt(len((t.ArbitraryData)))
	for i := range t.ArbitraryData {
		e.WritePrefixedBytes(t.ArbitraryData[i])
	}
}

// MarshalUploSize returns the encoded size of t.
func (t Transaction) MarshalUploSize() (size int) {
	size += 8
	for _, sci := range t.UplocoinInputs {
		size += len(sci.ParentID)
		size += sci.UnlockConditions.MarshalUploSize()
	}
	size += 8
	for _, sco := range t.UplocoinOutputs {
		size += sco.Value.MarshalUploSize()
		size += len(sco.UnlockHash)
	}
	size += 8
	for i := range t.FileContracts {
		size += t.FileContracts[i].MarshalUploSize()
	}
	size += 8
	for i := range t.FileContractRevisions {
		size += t.FileContractRevisions[i].MarshalUploSize()
	}
	size += 8
	for _, sp := range t.StorageProofs {
		size += len(sp.ParentID)
		size += len(sp.Segment)
		size += 8 + len(sp.HashSet)*crypto.HashSize
	}
	size += 8
	for _, sfi := range t.UplofundInputs {
		size += len(sfi.ParentID)
		size += len(sfi.ClaimUnlockHash)
		size += sfi.UnlockConditions.MarshalUploSize()
	}
	size += 8
	for _, sfo := range t.UplofundOutputs {
		size += sfo.Value.MarshalUploSize()
		size += len(sfo.UnlockHash)
		size += sfo.ClaimStart.MarshalUploSize()
	}
	size += 8
	for i := range t.MinerFees {
		size += t.MinerFees[i].MarshalUploSize()
	}
	size += 8
	for i := range t.ArbitraryData {
		size += 8 + len(t.ArbitraryData[i])
	}
	size += 8
	for _, ts := range t.TransactionSignatures {
		size += len(ts.ParentID)
		size += 8 // ts.PublicKeyIndex
		size += 8 // ts.Timelock
		size += ts.CoveredFields.MarshalUploSize()
		size += 8 + len(ts.Signature)
	}
	return
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (t *Transaction) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	t.UplocoinInputs = make([]UplocoinInput, d.NextPrefix(unsafe.Sizeof(UplocoinInput{})))
	for i := range t.UplocoinInputs {
		t.UplocoinInputs[i].UnmarshalUplo(d)
	}
	t.UplocoinOutputs = make([]UplocoinOutput, d.NextPrefix(unsafe.Sizeof(UplocoinOutput{})))
	for i := range t.UplocoinOutputs {
		t.UplocoinOutputs[i].UnmarshalUplo(d)
	}
	t.FileContracts = make([]FileContract, d.NextPrefix(unsafe.Sizeof(FileContract{})))
	for i := range t.FileContracts {
		t.FileContracts[i].UnmarshalUplo(d)
	}
	t.FileContractRevisions = make([]FileContractRevision, d.NextPrefix(unsafe.Sizeof(FileContractRevision{})))
	for i := range t.FileContractRevisions {
		t.FileContractRevisions[i].UnmarshalUplo(d)
	}
	t.StorageProofs = make([]StorageProof, d.NextPrefix(unsafe.Sizeof(StorageProof{})))
	for i := range t.StorageProofs {
		t.StorageProofs[i].UnmarshalUplo(d)
	}
	t.UplofundInputs = make([]UplofundInput, d.NextPrefix(unsafe.Sizeof(UplofundInput{})))
	for i := range t.UplofundInputs {
		t.UplofundInputs[i].UnmarshalUplo(d)
	}
	t.UplofundOutputs = make([]UplofundOutput, d.NextPrefix(unsafe.Sizeof(UplofundOutput{})))
	for i := range t.UplofundOutputs {
		t.UplofundOutputs[i].UnmarshalUplo(d)
	}
	t.MinerFees = make([]Currency, d.NextPrefix(unsafe.Sizeof(Currency{})))
	for i := range t.MinerFees {
		t.MinerFees[i].UnmarshalUplo(d)
	}
	t.ArbitraryData = make([][]byte, d.NextPrefix(unsafe.Sizeof([]byte{})))
	for i := range t.ArbitraryData {
		t.ArbitraryData[i] = d.ReadPrefixedBytes()
	}
	t.TransactionSignatures = make([]TransactionSignature, d.NextPrefix(unsafe.Sizeof(TransactionSignature{})))
	for i := range t.TransactionSignatures {
		t.TransactionSignatures[i].UnmarshalUplo(d)
	}
	return d.Err()
}

// MarshalJSON marshals an id as a hex string.
func (tid TransactionID) MarshalJSON() ([]byte, error) {
	return json.Marshal(tid.String())
}

// String prints the id in hex.
func (tid TransactionID) String() string {
	return fmt.Sprintf("%x", tid[:])
}

// UnmarshalJSON decodes the json hex string of the id.
func (tid *TransactionID) UnmarshalJSON(b []byte) error {
	return (*crypto.Hash)(tid).UnmarshalJSON(b)
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (ts TransactionSignature) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(ts.ParentID[:])
	e.WriteUint64(ts.PublicKeyIndex)
	e.WriteUint64(uint64(ts.Timelock))
	ts.CoveredFields.MarshalUplo(e)
	e.WritePrefixedBytes(ts.Signature)
	return e.Err()
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (ts *TransactionSignature) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	d.ReadFull(ts.ParentID[:])
	ts.PublicKeyIndex = d.NextUint64()
	ts.Timelock = BlockHeight(d.NextUint64())
	ts.CoveredFields.UnmarshalUplo(d)
	ts.Signature = d.ReadPrefixedBytes()
	return d.Err()
}

// MarshalUplo implements the encoding.UploMarshaler interface.
func (uc UnlockConditions) MarshalUplo(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.WriteUint64(uint64(uc.Timelock))
	e.WriteInt(len(uc.PublicKeys))
	for _, spk := range uc.PublicKeys {
		spk.MarshalUplo(e)
	}
	e.WriteUint64(uc.SignaturesRequired)
	return e.Err()
}

// MarshalUploSize returns the encoded size of uc.
func (uc UnlockConditions) MarshalUploSize() (size int) {
	size += 8 // Timelock
	size += 8 // length prefix for PublicKeys
	for _, spk := range uc.PublicKeys {
		size += len(spk.Algorithm)
		size += 8 + len(spk.Key)
	}
	size += 8 // SignaturesRequired
	return
}

// UnmarshalUplo implements the encoding.UploUnmarshaler interface.
func (uc *UnlockConditions) UnmarshalUplo(r io.Reader) error {
	d := encoding.NewDecoder(r, encoding.DefaultAllocLimit)
	uc.Timelock = BlockHeight(d.NextUint64())
	uc.PublicKeys = make([]UploPublicKey, d.NextPrefix(unsafe.Sizeof(UploPublicKey{})))
	for i := range uc.PublicKeys {
		uc.PublicKeys[i].UnmarshalUplo(d)
	}
	uc.SignaturesRequired = d.NextUint64()
	return d.Err()
}

// MarshalJSON is implemented on the unlock hash to always produce a hex string
// upon marshalling.
func (uh UnlockHash) MarshalJSON() ([]byte, error) {
	return json.Marshal(uh.String())
}

// UnmarshalJSON is implemented on the unlock hash to recover an unlock hash
// that has been encoded to a hex string.
func (uh *UnlockHash) UnmarshalJSON(b []byte) error {
	// Check the length of b.
	if len(b) != crypto.HashSize*2+UnlockHashChecksumSize*2+2 && len(b) != crypto.HashSize*2+2 {
		return ErrUnlockHashWrongLen
	}
	return uh.LoadString(string(b[1 : len(b)-1]))
}

// String returns the hex representation of the unlock hash as a string - this
// includes a checksum.
func (uh UnlockHash) String() string {
	uhChecksum := crypto.HashObject(uh)
	return fmt.Sprintf("%x%x", uh[:], uhChecksum[:UnlockHashChecksumSize])
}

// LoadString loads a hex representation (including checksum) of an unlock hash
// into an unlock hash object. An error is returned if the string is invalid or
// fails the checksum.
func (uh *UnlockHash) LoadString(strUH string) error {
	// Check the length of strUH.
	if len(strUH) != crypto.HashSize*2+UnlockHashChecksumSize*2 {
		return ErrUnlockHashWrongLen
	}

	// Decode the unlock hash.
	var byteUnlockHash []byte
	var checksum []byte
	_, err := fmt.Sscanf(strUH[:crypto.HashSize*2], "%x", &byteUnlockHash)
	if err != nil {
		return err
	}

	// Decode and verify the checksum.
	_, err = fmt.Sscanf(strUH[crypto.HashSize*2:], "%x", &checksum)
	if err != nil {
		return err
	}
	expectedChecksum := crypto.HashBytes(byteUnlockHash)
	if !bytes.Equal(expectedChecksum[:UnlockHashChecksumSize], checksum) {
		return ErrInvalidUnlockHashChecksum
	}

	copy(uh[:], byteUnlockHash[:])
	return nil
}

// Scan implements the fmt.Scanner interface, allowing UnlockHash values to be
// scanned from text.
func (uh *UnlockHash) Scan(s fmt.ScanState, ch rune) error {
	s.SkipSpace()
	tok, err := s.Token(false, nil)
	if err != nil {
		return err
	}
	return uh.LoadString(string(tok))
}

// MustParseAddress parses an address string to an UnlockHash, panicking
// if parsing fails.
//
// MustParseAddress should never be called on untrusted input; it is
// provided only for convenience when working with address strings that are
// known to be valid, such as the addresses in GenesisUplofundAllocation. To
// parse untrusted address strings, use the LoadString method of UnlockHash.
func MustParseAddress(addrStr string) (addr UnlockHash) {
	if err := addr.LoadString(addrStr); err != nil {
		panic(err)
	}
	return
}
