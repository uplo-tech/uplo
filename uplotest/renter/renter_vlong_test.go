package renter

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplofile"
	"github.com/uplo-tech/uplo/node"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/uplo/types"
)

// TestRenterSpendingReporting checks the accuracy for the reported
// spending
func TestRenterSpendingReporting(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup, creating without renter so the renter's
	// initial balance can be obtained
	groupParams := uplotest.GroupParams{
		Hosts:  2,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add a Renter node
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renterParams.SkipSetAllowance = true
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	r := nodes[0]

	// Get largest WindowSize from Hosts
	var windowSize types.BlockHeight
	for _, h := range tg.Hosts() {
		hg, err := h.HostGet()
		if err != nil {
			t.Fatal(err)
		}
		if hg.ExternalSettings.WindowSize >= windowSize {
			windowSize = hg.ExternalSettings.WindowSize
		}
	}

	// Get renter's initial Uplocoin balance
	wg, err := r.WalletGet()
	if err != nil {
		t.Fatal("Failed to get wallet:", err)
	}
	initialBalance := wg.ConfirmedUplocoinBalance

	// Set allowance
	if err = tg.SetRenterAllowance(r, uplotest.DefaultAllowance); err != nil {
		t.Fatal("Failed to set renter allowance:", err)
	}

	// Confirm Contracts were created as expected, check that the funds
	// allocated when setting the allowance are reflected correctly in the
	// wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err := uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, 0, 0)
		if err != nil {
			return err
		}
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Upload and download files to show spending
	var remoteFiles []*uplotest.RemoteFile
	for i := 0; i < 10; i++ {
		dataPieces := uint64(1)
		parityPieces := uint64(1)
		fileSize := 100 + uplotest.Fuzz()
		_, rf, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
		if err != nil {
			t.Fatal("Failed to upload a file for testing: ", err)
		}
		remoteFiles = append(remoteFiles, rf)
	}
	for _, rf := range remoteFiles {
		_, _, err = r.DownloadToDisk(rf, false)
		if err != nil {
			t.Fatal("Could not DownloadToDisk:", err)
		}
	}

	// Check to confirm upload and download spending was captured correctly
	// and reflected in the wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine blocks to force contract renewal
	if err = uplotest.RenewContractsByRenewWindow(r, tg); err != nil {
		t.Fatal(err)
	}

	// Confirm Contracts were renewed as expected
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err := uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, len(tg.Hosts()), 0)
		if err != nil {
			return err
		}
		rc, err := r.RenterContractsGet()
		if err != nil {
			return err
		}
		if err = uplotest.CheckRenewedContractsSpending(rc.ActiveContracts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine Block to confirm contracts and spending into blockchain
	m := tg.Miners()[0]
	if err = m.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Waiting for nodes to sync
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}

	// Check contract spending against reported spending
	rc, err := r.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	rcExpired, err := r.RenterExpiredContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if err = uplotest.CheckContractVsReportedSpending(r, windowSize, append(rc.InactiveContracts, rcExpired.ExpiredContracts...), rc.ActiveContracts); err != nil {
		t.Fatal(err)
	}

	// Check to confirm reported spending is still accurate with the renewed contracts
	// and reflected in the wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Record current Wallet Balance
	wg, err = r.WalletGet()
	if err != nil {
		t.Fatal("Failed to get wallet:", err)
	}
	initialPeriodEndBalance := wg.ConfirmedUplocoinBalance

	// Mine blocks to force contract renewal and new period
	cg, err := r.ConsensusGet()
	if err != nil {
		t.Fatal("Failed to get consensus:", err)
	}
	blockHeight := cg.Height
	endHeight := rc.ActiveContracts[0].EndHeight
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal("Failed to get renter:", err)
	}
	rw := rg.Settings.Allowance.RenewWindow
	for i := 0; i < int(endHeight-rw-blockHeight+types.MaturityDelay); i++ {
		if err = m.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}

	// Waiting for nodes to sync
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}

	// Check if Unspent unallocated funds were released after allowance period
	// was exceeded
	wg, err = r.WalletGet()
	if err != nil {
		t.Fatal("Failed to get wallet:", err)
	}
	if initialPeriodEndBalance.Cmp(wg.ConfirmedUplocoinBalance) > 0 {
		t.Fatal("Unspent Unallocated funds not released after contract renewal and maturity delay")
	}

	// Confirm Contracts were renewed as expected
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err := uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, len(tg.Hosts())*2, 0)
		if err != nil {
			return err
		}
		rc, err := r.RenterContractsGet()
		if err != nil {
			return err
		}
		if err = uplotest.CheckRenewedContractsSpending(rc.ActiveContracts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine Block to confirm contracts and spending on blockchain
	if err = m.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Waiting for nodes to sync
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}

	// Check contract spending against reported spending
	rc, err = r.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	rcExpired, err = r.RenterExpiredContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if err = uplotest.CheckContractVsReportedSpending(r, windowSize, append(rc.InactiveContracts, rcExpired.ExpiredContracts...), rc.ActiveContracts); err != nil {
		t.Fatal(err)
	}

	// Check to confirm reported spending is still accurate with the renewed contracts
	// and a new period and reflected in the wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Renew contracts by running out of funds
	_, err = uplotest.DrainContractsByUploading(r, tg)
	if err != nil {
		r.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Confirm Contracts were renewed as expected
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err := uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, len(tg.Hosts()), 0, len(tg.Hosts())*2, 0)
		if err != nil {
			return err
		}
		rc, err := r.RenterContractsGet()
		if err != nil {
			return err
		}
		if err = uplotest.CheckRenewedContractsSpending(rc.ActiveContracts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine Block to confirm contracts and spending on blockchain
	if err = m.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Waiting for nodes to sync
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}

	// Check contract spending against reported spending
	rc, err = r.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	rcExpired, err = r.RenterExpiredContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if err = uplotest.CheckContractVsReportedSpending(r, windowSize, append(rc.InactiveContracts, rcExpired.ExpiredContracts...), rc.ActiveContracts); err != nil {
		t.Fatal(err)
	}

	// Check to confirm reported spending is still accurate with the renewed contracts
	// and a new period and reflected in the wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine blocks to force contract renewal
	if err = uplotest.RenewContractsByRenewWindow(r, tg); err != nil {
		t.Fatal(err)
	}

	// Confirm Contracts were renewed as expected
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err := uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, len(tg.Hosts())*2, len(tg.Hosts()))
		if err != nil {
			return err
		}
		rc, err := r.RenterContractsGet()
		if err != nil {
			return err
		}
		if err = uplotest.CheckRenewedContractsSpending(rc.ActiveContracts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine Block to confirm contracts and spending into blockchain
	if err = m.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Waiting for nodes to sync
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}

	// Check contract spending against reported spending
	rc, err = r.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	rcExpired, err = r.RenterExpiredContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if err = uplotest.CheckContractVsReportedSpending(r, windowSize, append(rc.InactiveContracts, rcExpired.ExpiredContracts...), rc.ActiveContracts); err != nil {
		t.Fatal(err)
	}

	// Check to confirm reported spending is still accurate with the renewed contracts
	// and reflected in the wallet balance
	err = build.Retry(200, 100*time.Millisecond, func() error {
		err = uplotest.CheckBalanceVsSpending(r, initialBalance)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStresstestUploFileSet is a vlong test that performs multiple operations
// which modify the uplofileset in parallel for a period of time.
func TestStresstestUploFileSet(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	// Create a group for the test.
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterTestDir(t.Name()), groupParams)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Run the test for a set amount of time.
	timer := time.NewTimer(2 * time.Minute)
	stop := make(chan struct{})
	go func() {
		<-timer.C
		close(stop)
	}()
	wg := new(sync.WaitGroup)
	r := tg.Renters()[0]
	// Upload params.
	dataPieces := uint64(1)
	parityPieces := uint64(1)
	// Define helper error checking function
	//
	// isErr will ignore errors arising from paths not existing or path overloads.
	// This is because the goal of the stress test is to ensure that all the
	// creating, renaming, and deleting does not cause panics or race conditions.
	// It is almost guaranteed that the parallel threads will encounter these
	// types of errors but that is OK.
	isErr := func(err error) bool {
		if err == nil {
			return false
		}
		if strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
			return false
		}
		if strings.Contains(err.Error(), filesystem.ErrExists.Error()) {
			return false
		}
		if strings.Contains(err.Error(), uplofile.ErrUnknownPath.Error()) {
			return false
		}
		// Ignore os.IsNotExist errors
		if strings.Contains(err.Error(), "no such file or directory") {
			return false
		}
		if errors.Contains(err, uplotest.ErrFileNotTracked) {
			return false
		}
		return true
	}
	// One thread uploads new files.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Upload New File Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get a random directory to upload the file to.
			dirs, err := r.Dirs()
			if err != nil && !isErr(err) {
				continue
			}
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to get Dirs", threadName)))
				return
			}
			dir := dirs[fastrand.Intn(len(dirs))]
			sp, err := dir.Join(persist.RandomSuffix())
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: uplopath Join error", threadName)))
				return
			}
			// 30% chance for the file to be a 0-byte file.
			size := int(modules.SectorSize) + uplotest.Fuzz()
			if fastrand.Intn(3) == 0 {
				size = 0
			}
			// Upload the file
			lf, err := r.FilesDir().NewFile(size)
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to create NewFile", threadName)))
				return
			}
			rf, err := r.Upload(lf, sp, dataPieces, parityPieces, false)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to upload file", threadName)))
				return
			}
			err = r.WaitForUploadHealth(rf)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error with upload health", threadName)))
				return
			}
			time.Sleep(time.Duration(fastrand.Intn(1000))*time.Millisecond + time.Second) // between 1s and 2s
		}
	}()
	// One thread force uploads new files to an existing uplopath.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Force Upload Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get existing files and choose one randomly.
			files, err := r.Files(false)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error getting files", threadName)))
				return
			}
			// If there are no files we try again later.
			if len(files) == 0 {
				time.Sleep(time.Second)
				continue
			}
			// 30% chance for the file to be a 0-byte file.
			size := int(modules.SectorSize) + uplotest.Fuzz()
			if fastrand.Intn(3) == 0 {
				size = 0
			}
			// Upload the file.
			sp := files[fastrand.Intn(len(files))].UploPath
			lf, err := r.FilesDir().NewFile(size)
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to create NewFile", threadName)))
				return
			}
			rf, err := r.Upload(lf, sp, dataPieces, parityPieces, true)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to upload file", threadName)))
				return
			}
			err = r.WaitForUploadHealth(rf)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error with upload health", threadName)))
				return
			}
			time.Sleep(time.Duration(fastrand.Intn(4000))*time.Millisecond + time.Second) // between 4s and 5s
		}
	}()
	// One thread renames files and sometimes uploads a new file directly
	// afterwards.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Rename Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get existing files and choose one randomly.
			files, err := r.Files(false)
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error getting files", threadName)))
				return
			}
			// If there are no files we try again later.
			if len(files) == 0 {
				time.Sleep(time.Second)
				continue
			}
			sp := files[fastrand.Intn(len(files))].UploPath
			err = r.RenterRenamePost(sp, modules.RandomUploPath(), false)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error renaming file", threadName)))
				return
			}
			// 50% chance to replace renamed file with new one.
			if fastrand.Intn(2) == 0 {
				lf, err := r.FilesDir().NewFile(int(modules.SectorSize) + uplotest.Fuzz())
				if err != nil {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to create NewFile", threadName)))
					return
				}
				err = r.RenterUploadForcePost(lf.Path(), sp, dataPieces, parityPieces, false)
				if isErr(err) {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to upload file", threadName)))
					return
				}
			}
			time.Sleep(time.Duration(fastrand.Intn(4000))*time.Millisecond + time.Second) // between 4s and 5s
		}
	}()
	// One thread deletes files.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Delete Files Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get existing files and choose one randomly.
			files, err := r.Files(false)
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error getting files", threadName)))
				return
			}
			// If there are no files we try again later.
			if len(files) == 0 {
				time.Sleep(time.Second)
				continue
			}
			sp := files[fastrand.Intn(len(files))].UploPath
			err = r.RenterFileDeletePost(sp)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error deleting file", threadName)))
				return
			}
			time.Sleep(time.Duration(fastrand.Intn(5000))*time.Millisecond + time.Second) // between 5s and 6s
		}
	}()
	// One thread creates empty dirs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Create Empty Dirs Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get a random directory to create a dir in.
			dirs, err := r.Dirs()
			if err != nil && !isErr(err) {
				continue
			}
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error getting dirs", threadName)))
				return
			}
			dir := dirs[fastrand.Intn(len(dirs))]
			sp, err := dir.Join(persist.RandomSuffix())
			if err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: uplopath Join error", threadName)))
				return
			}
			err = r.RenterDirCreatePost(sp)
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error creating dir", threadName)))
				return
			}
			time.Sleep(time.Duration(fastrand.Intn(500))*time.Millisecond + 500*time.Millisecond) // between 0.5s and 1s
		}
	}()
	// One thread deletes a random directory and sometimes creates an empty one
	// in its place or simply renames it to be a sub dir of a random directory.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Delete Dir Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Get a random directory to delete.
			dirs, err := r.Dirs()
			if err != nil && !isErr(err) {
				continue
			}
			if isErr(err) {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to get Dirs", threadName)))
				return
			}
			dir := dirs[fastrand.Intn(len(dirs))]
			// Make sure that dir isn't the root.
			if dir.Equals(modules.RootUploPath()) {
				continue
			}
			if fastrand.Intn(2) == 0 {
				// 50% chance to delete and recreate the directory.
				err = r.RenterDirDeletePost(dir)
				if isErr(err) {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: unable to delete Dir", threadName)))
					return
				}
				err := r.RenterDirCreatePost(dir)
				// NOTE we could probably avoid ignoring ErrPathOverload if we
				// decided that `uplodir.New` returns a potentially existing
				// directory instead.
				if isErr(err) {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: error creating dir", threadName)))
					return
				}
			} else {
				// 50% chance to rename the directory to be the child of a
				// random existing directory.
				newParent := dirs[fastrand.Intn(len(dirs))]
				newDir, err := newParent.Join(persist.RandomSuffix())
				if err != nil {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: uplopath Join error", threadName)))
					return
				}
				if strings.HasPrefix(newDir.String(), dir.String()) {
					continue // can't rename folder into itself
				}
				err = r.RenterDirRenamePost(dir, newDir)
				if isErr(err) {
					t.Error(errors.AddContext(err, fmt.Sprintf("%v: error renaming dir", threadName)))
					return
				}
			}
			time.Sleep(time.Duration(fastrand.Intn(500))*time.Millisecond + 500*time.Millisecond) // between 0.5s and 1s
		}
	}()
	// One thread kills hosts to trigger repairs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		threadName := "Kill Host Thread"
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Break out if we only have dataPieces hosts left.
			hosts := tg.Hosts()
			if uint64(len(hosts)) == dataPieces {
				break
			}
			time.Sleep(10 * time.Second)
			// Choose random host.
			host := hosts[fastrand.Intn(len(hosts))]
			if err := tg.RemoveNode(host); err != nil {
				t.Error(errors.AddContext(err, fmt.Sprintf("%v: error removing host", threadName)))
				return
			}
		}
	}()
	// Wait until threads are done.
	wg.Wait()
}

// TestUploadStreamFailAndRepair kills an upload stream halfway through and
// repairs the file afterwards using the same endpoint.
func TestUploadStreamFailAndRepair(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:  2,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Add a renter with a dependency that causes an upload to fail after a certain
	// number of chunks.
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	deps := dependencies.NewDependencyDisruptUploadStream(5)
	renterParams.RenterDeps = deps
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Use upload streaming to upload a file. This should fail in the middle.
	data := fastrand.Bytes(int(10 * modules.SectorSize))
	sp := modules.RandomUploPath()
	deps.Fail()
	err = renter.RenterUploadStreamPost(bytes.NewReader(data), sp, 1, 1, false)
	deps.Disable()
	if err == nil {
		t.Fatal("upload streaming should fail but didn't")
	}
	// Redundancy should be 0 because the last chunk's upload was interrupted.
	fi, err := renter.RenterFileGet(sp)
	if err != nil {
		t.Fatal(err)
	}
	if fi.File.Redundancy != 0 {
		t.Fatalf("Expected redundancy to be 0 but was %v", fi.File.Redundancy)
	}
	// Repair the file.
	if err := renter.RenterUploadStreamRepairPost(bytes.NewReader(data), sp); err != nil {
		t.Fatal(err)
	}
	err = build.Retry(100, 100*time.Millisecond, func() error {
		fi, err = renter.RenterFileGet(sp)
		if err != nil {
			return err
		}
		// FileSize should be set correctly.
		if fi.File.Filesize != uint64(len(data)) {
			return fmt.Errorf("Filesize should be %v but was %v", len(data), fi.File.Filesize)
		}
		// Redundancy should be 2 after a successful repair.
		if fi.File.Redundancy != 2.0 {
			return fmt.Errorf("Expected redundancy to be 2.0 but was %v", fi.File.Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Make sure we can download the file.
	_, downloadedData, err := renter.RenterDownloadHTTPResponseGet(sp, 0, uint64(len(data)), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, downloadedData) {
		t.Fatal("downloaded data doesn't match uploaded data")
	}
}

// TestHostChurnUplofileDefragRegression tests that constant host churn won't
// ever stop the UploFile from being repaired to full health again.
func TestHostChurnUplofileDefragRegression(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Miners:  1,
		Renters: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Increase the renter's allowance to avoid complications during the test.
	r := tg.Renters()[0]
	a := uplotest.DefaultAllowance
	a.MaxPeriodChurn *= 100
	a.Funds = a.Funds.Mul64(100)
	a.Hosts = 200
	r.RenterPostAllowance(a)
	// Upload a file to all hosts.
	_, rf, err := r.UploadNewFileBlocking(100, 1, uint64(len(tg.Hosts())-1), false)
	if err != nil {
		t.Fatal(err)
	}
	// Take hosts offline.
	for _, host := range tg.Hosts() {
		if err := tg.RemoveNode(host); err != nil {
			t.Fatal(err)
		}
	}
	// Add hosts until we have had 100 hosts which all have repaired the file at
	// some point.
	// Go through 200 hosts
	for i := 0; i < 40; i++ {
		// Wait for redundancy to drop to 0.
		if err := r.WaitForDecreasingRedundancy(rf, 0); err != nil {
			t.Fatal(err)
		}
		// Spin up new hosts.
		_, err := tg.AddNodeN(node.HostTemplate, 5)
		if err != nil {
			t.Fatal(err)
		}
		// Health should go back up.
		if err := r.WaitForUploadHealth(rf); err != nil {
			t.Fatal(err)
		}
		// Take hosts offline.
		for _, host := range tg.Hosts() {
			if err := tg.RemoveNode(host); err != nil {
				t.Fatal(err)
			}
		}
	}
}
