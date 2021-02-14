package consensus

import (
	"testing"

	"github.com/uplo-tech/bolt"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// TestCommitDelayedUplocoinOutputDiffBadMaturity commits a delayed Uplocoin
// output that has a bad maturity height and triggers a panic.
func TestCommitDelayedUplocoinOutputDiffBadMaturity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting error after corrupting database")
		}
	}()

	// Commit a delayed Uplocoin output with maturity height = cs.height()+1
	maturityHeight := cst.cs.dbBlockHeight() - 1
	id := types.UplocoinOutputID{'1'}
	dsco := types.UplocoinOutput{Value: types.NewCurrency64(1)}
	dscod := modules.DelayedUplocoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             id,
		UplocoinOutput:  dsco,
		MaturityHeight: maturityHeight,
	}
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitDelayedUplocoinOutputDiff(tx, dscod, modules.DiffApply)
		return nil
	})
}

// TestCommitNodeDiffs probes the commitNodeDiffs method of the consensus set.
/*
func TestCommitNodeDiffs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
  	if err := cst.Close(); err != nil {
  		t.Fatal(err)
 	}
}()
	pb := cst.cs.dbCurrentProcessedBlock()
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitDiffSet(tx, pb, modules.DiffRevert) // pull the block node out of the consensus set.
		return nil
	})

	// For diffs that can be destroyed in the same block they are created,
	// create diffs that do just that. This has in the past caused issues upon
	// rewinding.
	scoid := types.UplocoinOutputID{'1'}
	scod0 := modules.UplocoinOutputDiff{
		Direction: modules.DiffApply,
		ID:        scoid,
	}
	scod1 := modules.UplocoinOutputDiff{
		Direction: modules.DiffRevert,
		ID:        scoid,
	}
	fcid := types.FileContractID{'2'}
	fcd0 := modules.FileContractDiff{
		Direction: modules.DiffApply,
		ID:        fcid,
	}
	fcd1 := modules.FileContractDiff{
		Direction: modules.DiffRevert,
		ID:        fcid,
	}
	sfoid := types.UplofundOutputID{'3'}
	sfod0 := modules.UplofundOutputDiff{
		Direction: modules.DiffApply,
		ID:        sfoid,
	}
	sfod1 := modules.UplofundOutputDiff{
		Direction: modules.DiffRevert,
		ID:        sfoid,
	}
	dscoid := types.UplocoinOutputID{'4'}
	dscod := modules.DelayedUplocoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             dscoid,
		MaturityHeight: cst.cs.dbBlockHeight() + types.MaturityDelay,
	}
	var uplofundPool types.Currency
	err = cst.cs.db.Update(func(tx *bolt.Tx) error {
		uplofundPool = getUplofundPool(tx)
		return nil
	})
	if err != nil {
		panic(err)
	}
	sfpd := modules.UplofundPoolDiff{
		Direction: modules.DiffApply,
		Previous:  uplofundPool,
		Adjusted:  uplofundPool.Add(types.NewCurrency64(1)),
	}
	pb.UplocoinOutputDiffs = append(pb.UplocoinOutputDiffs, scod0)
	pb.UplocoinOutputDiffs = append(pb.UplocoinOutputDiffs, scod1)
	pb.FileContractDiffs = append(pb.FileContractDiffs, fcd0)
	pb.FileContractDiffs = append(pb.FileContractDiffs, fcd1)
	pb.UplofundOutputDiffs = append(pb.UplofundOutputDiffs, sfod0)
	pb.UplofundOutputDiffs = append(pb.UplofundOutputDiffs, sfod1)
	pb.DelayedUplocoinOutputDiffs = append(pb.DelayedUplocoinOutputDiffs, dscod)
	pb.UplofundPoolDiffs = append(pb.UplofundPoolDiffs, sfpd)
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		createUpcomingDelayedOutputMaps(tx, pb, modules.DiffApply)
		return nil
	})
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitNodeDiffs(tx, pb, modules.DiffApply)
		return nil
	})
	exists := cst.cs.db.inUplocoinOutputs(scoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inFileContracts(fcid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inUplofundOutputs(sfoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitNodeDiffs(tx, pb, modules.DiffRevert)
		return nil
	})
	exists = cst.cs.db.inUplocoinOutputs(scoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inFileContracts(fcid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inUplofundOutputs(sfoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
}
*/

/*
// TestUplocoinOutputDiff applies and reverts a Uplocoin output diff, then
// triggers an inconsistency panic.
func TestCommitUplocoinOutputDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
  	if err := cst.Close(); err != nil {
  		t.Fatal(err)
 	}
}()

	// Commit a Uplocoin output diff.
	initialScosLen := cst.cs.db.lenUplocoinOutputs()
	id := types.UplocoinOutputID{'1'}
	sco := types.UplocoinOutput{Value: types.NewCurrency64(1)}
	scod := modules.UplocoinOutputDiff{
		Direction:     modules.DiffApply,
		ID:            id,
		UplocoinOutput: sco,
	}
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffApply)
	if cst.cs.db.lenUplocoinOutputs() != initialScosLen+1 {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	if cst.cs.db.getUplocoinOutputs(id).Value.Cmp(sco.Value) != 0 {
		t.Error("wrong Uplocoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffRevert)
	if cst.cs.db.lenUplocoinOutputs() != initialScosLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inUplocoinOutputs(id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffApply)
	scod.Direction = modules.DiffRevert
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffApply)
	if cst.cs.db.lenUplocoinOutputs() != initialScosLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inUplocoinOutputs(id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffRevert)
	if cst.cs.db.lenUplocoinOutputs() != initialScosLen+1 {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	if cst.cs.db.getUplocoinOutputs(id).Value.Cmp(sco.Value) != 0 {
		t.Error("wrong Uplocoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitUplocoinOutputDiff {
			t.Error("expecting errBadCommitUplocoinOutputDiff, got", r)
		}
	}()
	// Try reverting a revert diff that was already reverted. (add an object
	// that already exists)
	cst.cs.commitUplocoinOutputDiff(scod, modules.DiffRevert)
}
*/

/*
// TestCommitFileContracttDiff applies and reverts a file contract diff, then
// triggers an inconsistency panic.
func TestCommitFileContractDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Commit a file contract diff.
	initialFcsLen := cst.cs.db.lenFileContracts()
	id := types.FileContractID{'1'}
	fc := types.FileContract{Payout: types.NewCurrency64(1)}
	fcd := modules.FileContractDiff{
		Direction:    modules.DiffApply,
		ID:           id,
		FileContract: fc,
	}
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	if cst.cs.db.lenFileContracts() != initialFcsLen+1 {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	if cst.cs.db.getFileContracts(id).Payout.Cmp(fc.Payout) != 0 {
		t.Error("wrong Uplocoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert)
	if cst.cs.db.lenFileContracts() != initialFcsLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inFileContracts(id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	fcd.Direction = modules.DiffRevert
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	if cst.cs.db.lenFileContracts() != initialFcsLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inFileContracts(id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert)
	if cst.cs.db.lenFileContracts() != initialFcsLen+1 {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	if cst.cs.db.getFileContracts(id).Payout.Cmp(fc.Payout) != 0 {
		t.Error("wrong Uplocoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitFileContractDiff {
			t.Error("expecting errBadCommitFileContractDiff, got", r)
		}
	}()
	// Try reverting an apply diff that was already reverted. (remove an object
	// that was already removed)
	fcd.Direction = modules.DiffApply                      // Object currently exists, but make the direction 'apply'.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert) // revert the application.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert) // revert the application again, in error.
}
*/

// TestUplofundOutputDiff applies and reverts a uplofund output diff, then
// triggers an inconsistency panic.
/*
func TestCommitUplofundOutputDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Commit a uplofund output diff.
	initialScosLen := cst.cs.db.lenUplofundOutputs()
	id := types.UplofundOutputID{'1'}
	sfo := types.UplofundOutput{Value: types.NewCurrency64(1)}
	sfod := modules.UplofundOutputDiff{
		Direction:     modules.DiffApply,
		ID:            id,
		UplofundOutput: sfo,
	}
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffApply)
	if cst.cs.db.lenUplofundOutputs() != initialScosLen+1 {
		t.Error("uplofund output diff set did not increase in size")
	}
	sfo1 := cst.cs.db.getUplofundOutputs(id)
	if sfo1.Value.Cmp(sfo.Value) != 0 {
		t.Error("wrong uplofund output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffRevert)
	if cst.cs.db.lenUplofundOutputs() != initialScosLen {
		t.Error("uplofund output diff set did not increase in size")
	}
	exists := cst.cs.db.inUplofundOutputs(id)
	if exists {
		t.Error("uplofund output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffApply)
	sfod.Direction = modules.DiffRevert
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffApply)
	if cst.cs.db.lenUplofundOutputs() != initialScosLen {
		t.Error("uplofund output diff set did not increase in size")
	}
	exists = cst.cs.db.inUplofundOutputs(id)
	if exists {
		t.Error("uplofund output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffRevert)
	if cst.cs.db.lenUplofundOutputs() != initialScosLen+1 {
		t.Error("uplofund output diff set did not increase in size")
	}
	sfo2 := cst.cs.db.getUplofundOutputs(id)
	if sfo2.Value.Cmp(sfo.Value) != 0 {
		t.Error("wrong uplofund output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitUplofundOutputDiff {
			t.Error("expecting errBadCommitUplofundOutputDiff, got", r)
		}
	}()
	// Try applying a revert diff that was already applied. (remove an object
	// that was already removed)
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffApply) // Remove the object.
	cst.cs.commitUplofundOutputDiff(sfod, modules.DiffApply) // Remove the object again.
}
*/

// TestCommitDelayedUplocoinOutputDiff probes the commitDelayedUplocoinOutputDiff
// method of the consensus set.
/*
func TestCommitDelayedUplocoinOutputDiff(t *testing.T) {
	t.Skip("test isn't working, but checks the wrong code anyway")
	if testing.Short() {
		t.Skip()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Commit a delayed Uplocoin output with maturity height = cs.height()+1
	maturityHeight := cst.cs.height() + 1
	initialDscosLen := cst.cs.db.lenDelayedUplocoinOutputsHeight(maturityHeight)
	id := types.UplocoinOutputID{'1'}
	dsco := types.UplocoinOutput{Value: types.NewCurrency64(1)}
	dscod := modules.DelayedUplocoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             id,
		UplocoinOutput:  dsco,
		MaturityHeight: maturityHeight,
	}
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffApply)
	if cst.cs.db.lenDelayedUplocoinOutputsHeight(maturityHeight) != initialDscosLen+1 {
		t.Fatal("delayed output diff set did not increase in size")
	}
	if cst.cs.db.getDelayedUplocoinOutputs(maturityHeight, id).Value.Cmp(dsco.Value) != 0 {
		t.Error("wrong delayed Uplocoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffRevert)
	if cst.cs.db.lenDelayedUplocoinOutputsHeight(maturityHeight) != initialDscosLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inDelayedUplocoinOutputsHeight(maturityHeight, id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffApply)
	dscod.Direction = modules.DiffRevert
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffApply)
	if cst.cs.db.lenDelayedUplocoinOutputsHeight(maturityHeight) != initialDscosLen {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inDelayedUplocoinOutputsHeight(maturityHeight, id)
	if exists {
		t.Error("Uplocoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffRevert)
	if cst.cs.db.lenDelayedUplocoinOutputsHeight(maturityHeight) != initialDscosLen+1 {
		t.Error("Uplocoin output diff set did not increase in size")
	}
	if cst.cs.db.getDelayedUplocoinOutputs(maturityHeight, id).Value.Cmp(dsco.Value) != 0 {
		t.Error("wrong Uplocoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitDelayedUplocoinOutputDiff {
			t.Error("expecting errBadCommitDelayedUplocoinOutputDiff, got", r)
		}
	}()
	// Try applying an apply diff that was already applied. (add an object
	// that already exists)
	dscod.Direction = modules.DiffApply                             // set the direction to apply
	cst.cs.commitDelayedUplocoinOutputDiff(dscod, modules.DiffApply) // apply an already existing delayed output.
}
*/

/*
// TestCommitUplofundPoolDiff probes the commitUplofundPoolDiff method of the
// consensus set.
func TestCommitUplofundPoolDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Apply two uplofund pool diffs, and then a diff with 0 change. Then revert
	// them all.
	initial := cst.cs.uplofundPool
	adjusted1 := initial.Add(types.NewCurrency64(200))
	adjusted2 := adjusted1.Add(types.NewCurrency64(500))
	adjusted3 := adjusted2.Add(types.NewCurrency64(0))
	sfpd1 := modules.UplofundPoolDiff{
		Direction: modules.DiffApply,
		Previous:  initial,
		Adjusted:  adjusted1,
	}
	sfpd2 := modules.UplofundPoolDiff{
		Direction: modules.DiffApply,
		Previous:  adjusted1,
		Adjusted:  adjusted2,
	}
	sfpd3 := modules.UplofundPoolDiff{
		Direction: modules.DiffApply,
		Previous:  adjusted2,
		Adjusted:  adjusted3,
	}
	cst.cs.commitUplofundPoolDiff(sfpd1, modules.DiffApply)
	if cst.cs.uplofundPool.Cmp(adjusted1) != 0 {
		t.Error("uplofund pool was not adjusted correctly")
	}
	cst.cs.commitUplofundPoolDiff(sfpd2, modules.DiffApply)
	if cst.cs.uplofundPool.Cmp(adjusted2) != 0 {
		t.Error("second uplofund pool adjustment was flawed")
	}
	cst.cs.commitUplofundPoolDiff(sfpd3, modules.DiffApply)
	if cst.cs.uplofundPool.Cmp(adjusted3) != 0 {
		t.Error("second uplofund pool adjustment was flawed")
	}
	cst.cs.commitUplofundPoolDiff(sfpd3, modules.DiffRevert)
	if cst.cs.uplofundPool.Cmp(adjusted2) != 0 {
		t.Error("reverting second adjustment was flawed")
	}
	cst.cs.commitUplofundPoolDiff(sfpd2, modules.DiffRevert)
	if cst.cs.uplofundPool.Cmp(adjusted1) != 0 {
		t.Error("reverting second adjustment was flawed")
	}
	cst.cs.commitUplofundPoolDiff(sfpd1, modules.DiffRevert)
	if cst.cs.uplofundPool.Cmp(initial) != 0 {
		t.Error("reverting first adjustment was flawed")
	}

	// Do a chaining set of panics. First apply a negative pool adjustment,
	// then revert the pool diffs in the wrong order, than apply the pool diffs
	// in the wrong order.
	defer func() {
		r := recover()
		if r != errApplyUplofundPoolDiffMismatch {
			t.Error("expecting errApplyUplofundPoolDiffMismatch, got", r)
		}
	}()
	defer func() {
		r := recover()
		if r != errRevertUplofundPoolDiffMismatch {
			t.Error("expecting errRevertUplofundPoolDiffMismatch, got", r)
		}
		cst.cs.commitUplofundPoolDiff(sfpd1, modules.DiffApply)
	}()
	defer func() {
		r := recover()
		if r != errNonApplyUplofundPoolDiff {
			t.Error(r)
		}
		cst.cs.commitUplofundPoolDiff(sfpd1, modules.DiffRevert)
	}()
	defer func() {
		r := recover()
		if r != errNegativePoolAdjustment {
			t.Error("expecting errNegativePoolAdjustment, got", r)
		}
		sfpd2.Direction = modules.DiffRevert
		cst.cs.commitUplofundPoolDiff(sfpd2, modules.DiffApply)
	}()
	cst.cs.commitUplofundPoolDiff(sfpd1, modules.DiffApply)
	cst.cs.commitUplofundPoolDiff(sfpd2, modules.DiffApply)
	negativeAdjustment := adjusted2.Sub(types.NewCurrency64(100))
	negativeSfpd := modules.UplofundPoolDiff{
		Previous: adjusted3,
		Adjusted: negativeAdjustment,
	}
	cst.cs.commitUplofundPoolDiff(negativeSfpd, modules.DiffApply)
}
*/

/*
// TestDeleteObsoleteDelayedOutputMapsSanity probes the sanity checks of the
// deleteObsoleteDelayedOutputMaps method of the consensus set.
func TestDeleteObsoleteDelayedOutputMapsSanity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	pb := cst.cs.currentProcessedBlock()
	err = cst.cs.db.Update(func(tx *bolt.Tx) error {
		return commitDiffSet(tx, pb, modules.DiffRevert)
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting an error after corrupting the database")
		}
	}()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting an error after corrupting the database")
		}

		// Trigger a panic by deleting a map with outputs in it during revert.
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return createUpcomingDelayedOutputMaps(tx, pb, modules.DiffApply)
		})
		if err != nil {
			t.Fatal(err)
		}
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return commitNodeDiffs(tx, pb, modules.DiffApply)
		})
		if err != nil {
			t.Fatal(err)
		}
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return deleteObsoleteDelayedOutputMaps(tx, pb, modules.DiffRevert)
		})
		if err != nil {
			t.Fatal(err)
		}
	}()

	// Trigger a panic by deleting a map with outputs in it during apply.
	err = cst.cs.db.Update(func(tx *bolt.Tx) error {
		return deleteObsoleteDelayedOutputMaps(tx, pb, modules.DiffApply)
	})
	if err != nil {
		t.Fatal(err)
	}
}
*/

/*
// TestGenerateAndApplyDiffSanity triggers the sanity checks in the
// generateAndApplyDiff method of the consensus set.
func TestGenerateAndApplyDiffSanity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	pb := cst.cs.currentProcessedBlock()
	cst.cs.commitDiffSet(pb, modules.DiffRevert)

	defer func() {
		r := recover()
		if r != errRegenerateDiffs {
			t.Error("expected errRegenerateDiffs, got", r)
		}
	}()
	defer func() {
		r := recover()
		if r != errInvalidSuccessor {
			t.Error("expected errInvalidSuccessor, got", r)
		}

		// Trigger errRegenerteDiffs
		_ = cst.cs.generateAndApplyDiff(pb)
	}()

	// Trigger errInvalidSuccessor
	parent := cst.cs.db.getBlockMap(pb.Parent)
	parent.DiffsGenerated = false
	_ = cst.cs.generateAndApplyDiff(parent)
}
*/
