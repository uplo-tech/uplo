package host

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
)

// TestAccountCallDeposit verifies we can deposit into an ephemeral account
func TestAccountCallDeposit(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	_, accountID := prepareAccount()

	// Deposit money into it
	diff := types.NewCurrency64(100)
	before := getAccountBalance(am, accountID)
	err = callDeposit(am, accountID, diff)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the amount was credited
	after := getAccountBalance(am, accountID)
	if !after.Sub(before).Equals(diff) {
		t.Fatal("Deposit was not credited")
	}
}

// TestAccountMaxBalance verifies we can never deposit more than the account max
// balance into an ephemeral account.
func TestAccountMaxBalance(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	_, accountID := prepareAccount()

	// Verify the deposit can not exceed the max account balance
	maxBalance := am.h.InternalSettings().MaxEphemeralAccountBalance
	exceedingBalance := maxBalance.Add(types.NewCurrency64(1))
	err = callDeposit(am, accountID, exceedingBalance)
	if !errors.Contains(err, ErrBalanceMaxExceeded) {
		t.Fatal(err)
	}
	// A refund should ignore the max account balance.
	err = am.callRefund(accountID, exceedingBalance)
	if err != nil {
		t.Fatal(err)
	}
}

// TestAccountCallWithdraw verifies we can withdraw from an ephemeral account.
func TestAccountCallWithdraw(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()

	// Fund the account
	err = callDeposit(am, accountID, types.NewCurrency64(10))
	if err != nil {
		t.Fatal(err)
	}

	// Prepare a withdrawal message
	amount := types.NewCurrency64(5)
	msg, sig := prepareWithdrawal(accountID, amount, am.h.blockHeight, sk)

	// Spend half of it and verify account balance
	err = callWithdraw(am, msg, sig)
	if err != nil {
		t.Fatal(err)
	}

	// Verify current balance
	current := types.NewCurrency64(5)
	balance := getAccountBalance(am, accountID)
	if !balance.Equals(current) {
		t.Fatal("Account balance was incorrect after spend")
	}

	// Overspend, followed by a deposit to verify the account properly blocks
	// to await the deposit and then resolves.
	overSpend := types.NewCurrency64(7)
	deposit := types.NewCurrency64(3)
	expected := current.Add(deposit).Sub(overSpend)

	var atomicErrs uint64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		msg, sig = prepareWithdrawal(accountID, overSpend, am.h.blockHeight, sk)
		if err := callWithdraw(am, msg, sig); err != nil {
			t.Log(err)
			atomic.AddUint64(&atomicErrs, 1)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond) // ensure withdrawal blocks
		if err := callDeposit(am, accountID, deposit); err != nil {
			t.Log(err)
			atomic.AddUint64(&atomicErrs, 1)
		}
	}()
	wg.Wait()

	if atomic.LoadUint64(&atomicErrs) != 0 {
		t.Fatal("Unexpected error occurred during blocked withdrawal")
	}

	balance = getAccountBalance(am, accountID)
	if !balance.Equals(expected) {
		t.Fatal("Account balance was incorrect after spend", balance.HumanString())
	}
}

// TestAccountCallWithdrawTimeout verifies withdrawals timeout if the account
// is not sufficiently funded.
func TestAccountCallWithdrawTimeout(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare a new account
	sk, unknown := prepareAccount()

	// Withdraw from it
	amount := types.NewCurrency64(1)
	msg, sig := prepareWithdrawal(unknown, amount, am.h.blockHeight, sk)
	if err := callWithdraw(am, msg, sig); !errors.Contains(err, ErrBalanceInsufficient) {
		t.Fatal("Unexpected error: ", err)
	}
}

// TestAccountExpiry verifies accounts expire and get pruned.
func TestAccountExpiry(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	ht, err := blankMockHostTester(&dependencies.HostExpireEphemeralAccounts{}, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	_, accountID := prepareAccount()

	// Deposit some money into the account
	if err = build.Retry(3, 100*time.Millisecond, func() error {
		return callDeposit(am, accountID, types.NewCurrency64(10))
	}); err != nil {
		t.Fatal(err)
	}

	// Verify the balance
	balance := getAccountBalance(am, accountID)
	if !balance.Equals(types.NewCurrency64(10)) {
		t.Fatal("Account balance was incorrect after deposit")
	}

	// Verify the account got pruned
	if err = build.Retry(3, pruneExpiredAccountsFrequency, func() error {
		balance = getAccountBalance(am, accountID)
		if !balance.IsZero() {
			return errors.New("Account balance was incorrect after expiry")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAccountWithdrawalSpent verifies a withdrawal can not be spent twice.
func TestAccountWithdrawalSpent(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()

	// Fund the account
	err = callDeposit(am, accountID, types.NewCurrency64(10))
	if err != nil {
		t.Fatal(err)
	}

	// Prepare a withdrawal message
	diff := types.NewCurrency64(5)
	msg, sig := prepareWithdrawal(accountID, diff, am.h.blockHeight+10, sk)
	err = callWithdraw(am, msg, sig)
	if err != nil {
		t.Fatal(err)
	}

	err = callWithdraw(am, msg, sig)
	if !errors.Contains(err, ErrWithdrawalSpent) {
		t.Fatal("Expected withdrawal spent error", err)
	}
}

// TestAccountWithdrawalExpired verifies a withdrawal with an expiry in the past
// is not accepted.
func TestAccountWithdrawalExpired(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := newHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()

	// Fund the account
	err = callDeposit(am, accountID, types.NewCurrency64(10))
	if err != nil {
		t.Fatal(err)
	}

	// Prepare a withdrawal message
	diff := types.NewCurrency64(5)
	msg, sig := prepareWithdrawal(accountID, diff, am.h.blockHeight-1, sk)
	err = callWithdraw(am, msg, sig)
	if !errors.Contains(err, modules.ErrWithdrawalExpired) {
		t.Fatal("Expected withdrawal expired error", err)
	}
}

// TestAccountWithdrawalExtremeFuture verifies a withdrawal with an expiry in
// the extreme future is not accepted
func TestAccountWithdrawalExtremeFuture(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()

	// Fund the account
	err = callDeposit(am, accountID, types.NewCurrency64(10))
	if err != nil {
		t.Fatal(err)
	}

	// Make sure to test the cutoff point properly
	shouldNotExpire := am.h.blockHeight + bucketBlockRange
	shouldExpire := shouldNotExpire + 1

	// Prepare a withdrawal message
	oneCurrency := types.NewCurrency64(1)
	msg, sig := prepareWithdrawal(accountID, oneCurrency, shouldExpire, sk)
	err = callWithdraw(am, msg, sig)
	if !errors.Contains(err, modules.ErrWithdrawalExtremeFuture) {
		t.Fatal("Expected withdrawal extreme future error", err)
	}

	msg, sig = prepareWithdrawal(accountID, oneCurrency, shouldNotExpire, sk)
	err = callWithdraw(am, msg, sig)
	if errors.Contains(err, modules.ErrWithdrawalExtremeFuture) {
		t.Fatal("Expected withdrawal to be valid", err)
	}
}

// TestAccountWithdrawalInvalidSignature verifies a withdrawal with an invalid
// signature is not accepted
func TestAccountWithdrawalInvalidSignature(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account and fund it
	sk1, accountID1 := prepareAccount()
	err = callDeposit(am, accountID1, types.NewCurrency64(10))
	if err != nil {
		t.Fatal(err)
	}

	// Prepare a withdrawal message
	diff := types.NewCurrency64(5)
	msg1, _ := prepareWithdrawal(accountID1, diff, am.h.blockHeight+5, sk1)

	// Prepare another account and sign the same message using the other account
	sk2, _ := prepareAccount()
	_, sig2 := prepareWithdrawal(accountID1, diff, am.h.blockHeight+5, sk2)

	err = callWithdraw(am, msg1, sig2)
	if !errors.Contains(err, modules.ErrWithdrawalInvalidSignature) {
		t.Fatal("Expected withdrawal invalid signature error", err)
	}
}

// TestAccountRiskBenchmark benches the account manager and tries to reach max
// risk. If it can reach max risk it prints the configuration that managed to
// reach it. This test should be skipped, it is added for documenting purposes.
func TestAccountRiskBenchmark(t *testing.T) {
	t.SkipNow()

	// Create a host with the maxrisk dependency. This will ensure the host
	// returns errMaxRiskReached when a withdraw is blocked because maxrisk is
	// reached. We use this to be aware that maxrisk is reached. The latency is
	// set to zero to ensure we do not add unwanted latency when persisting data
	// to disk.
	deps := dependencies.NewHostMaxEphemeralAccountRiskReached(0)
	ht, err := newMockHostTester(deps, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// These atomics cause the test to stop, if we encounter errors we want to
	// stop, if we encounter max risk we have succeeded in our goal to push the
	// account manager to reaching max risk.
	var atomicMaxRiskReached uint64
	var atomicWithdrawalErrs uint64
	var atomicTestErrs uint64

	// Mine a block every second to ensure we rotate the fingerprints and
	// prevent this benchmark from consuming GBs of memory. This is also
	// necessary to test if fingerprint rotation causes the account manager to
	// grind to a halt.
	atomicBlockHeight := uint64(ht.host.blockHeight)
	doneChan := make(chan struct{})
	go func() {
		for {
			select {
			case <-doneChan:
				break
			case <-time.After(time.Second):
				if _, err := ht.miner.AddBlock(); err != nil {
					atomic.AddUint64(&atomicTestErrs, 1)
					break
				}
				atomic.AddUint64(&atomicBlockHeight, 1)
				continue
			}
		}
	}()

	// The benchmark configuration has 3 variables, #accounts, #withdrawals and
	// #threads. The number of accounts will be fixed for this benchmark, the
	// number of withdrawals and threads are multiplied by a factor as long as
	// max risk is not reached.
	acc := 10
	withdrawals := 100000
	threads := 16

	// Grab some settings
	his := ht.host.InternalSettings()
	maxBalance := his.MaxEphemeralAccountBalance
	maxRisk := his.MaxEphemeralAccountRisk
	withdrawalSize := maxBalance.Div64(10000)

	// Prepare the accounts
	accountIDs := make([]modules.AccountID, acc)
	accountSKs := make([]crypto.SecretKey, acc)
	accountBal := maxBalance
	for a := 0; a < acc; a++ {
		sk, accountID := prepareAccount()
		accountIDs[a] = accountID
		accountSKs[a] = sk
		err = callDeposit(am, accountID, accountBal)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Note we can not prepare the withdraw messages and signatures for this
	// benchmark because of the fact that blocks are mined and blockheight is
	// changing.

	// Spin up a routine that periodically refills the accounts with the amount
	// that has been withdrawn, we do this to ensure a "safe" deposit that never
	// reaches the max balance, and also ensures we always perfectly top off the
	// account.
	atomicWithdrawn := make([]uint64, acc)
	go func() {
		for {
			select {
			case <-doneChan:
				break
			case <-time.After(100 * time.Millisecond):
				for a := 0; a < acc; a++ {
					deposit := atomic.LoadUint64(&atomicWithdrawn[a])
					if err := callDeposit(am, accountIDs[a], types.NewCurrency64(deposit)); err != nil {
						atomic.AddUint64(&atomicTestErrs, 1)
						return
					}
					atomic.StoreUint64(&atomicWithdrawn[a], 0)
				}
			}
		}
	}()

	// Spin a goroutine that logs the current withdrawal count every second.
	// This is mostly to verify we have not run into a deadlock when this
	// benchmark runs into the high figures.
	var atomicWithdrawals uint64
	go func() {
		for {
			select {
			case <-doneChan:
				break
			case <-time.After(3 * time.Second):
				wc := atomic.LoadUint64(&atomicWithdrawals)
				am.mu.Lock()
				cr := am.currentRisk.HumanString()
				mr := maxRisk.HumanString()
				am.mu.Unlock()
				fmt.Printf("\n\tNum Withdrawals: %v\n\tCurrent Risk: %v\n\tMax Risk: %v\n\n", wc, cr, mr)
			}
		}
	}()

	// Keep running until maxrisk is reached
	for {
		atomic.StoreUint64(&atomicWithdrawals, 0) // reset counter

		// Log the configuration
		fmt.Printf("- - - - - - - - \nConfiguration:\nAccounts: %d\nWithdrawals: %d\nThreads: %d\n- - - - - - - - \n\n", acc, withdrawals, threads)

		var wg sync.WaitGroup
		for th := 0; th < threads; th++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for w := 0; w < withdrawals/threads; w++ {
					randIndex := fastrand.Intn(len(accountIDs))
					msg, sig := prepareWithdrawal(accountIDs[randIndex], withdrawalSize, types.BlockHeight(atomic.LoadUint64(&atomicBlockHeight)+bucketBlockRange/2), accountSKs[randIndex])

					withdrawn, _ := withdrawalSize.Uint64()
					atomic.AddUint64(&atomicWithdrawn[randIndex], withdrawn)

					wErr := callWithdraw(am, msg, sig)
					if errors.Contains(wErr, errMaxRiskReached) {
						atomic.StoreUint64(&atomicMaxRiskReached, 1)
					} else if errors.Contains(wErr, ErrBalanceInsufficient) {
						if dErr := callDeposit(am, accountIDs[randIndex], maxBalance); dErr != nil {
							atomic.AddUint64(&atomicWithdrawalErrs, 1)
							t.Log(wErr)
							break
						}
					} else if wErr != nil {
						atomic.AddUint64(&atomicWithdrawalErrs, 1)
						t.Log(wErr)
						break
					}
					atomic.AddUint64(&atomicWithdrawals, 1)

					// Sleep to allow the GC some time. If we do not sleep here,
					// the percentile statistics are much worse due to outliers.
					time.Sleep(10 * time.Microsecond)
				}
			}()
		}
		wg.Wait()

		if atomic.LoadUint64(&atomicTestErrs) > 0 ||
			atomic.LoadUint64(&atomicWithdrawalErrs) > 0 ||
			atomic.LoadUint64(&atomicMaxRiskReached) != 0 {
			break
		}

		withdrawals *= 2
		if threads < 128 {
			threads *= 2
		}
	}
	doneChan <- struct{}{}

	numErrors := atomic.LoadUint64(&atomicWithdrawalErrs)
	if numErrors > 0 {
		t.Fatalf("%v withdrawals errors\n", numErrors)
	}

	maxRiskReached := atomic.LoadUint64(&atomicMaxRiskReached) == 1
	if maxRiskReached {
		t.Log("MaxRisk was reached")
	}
}

// TestAccountWithdrawalBenchmark benches the withdrawals by running a couple of
// configurations (#accounts,#withdrawals,#threads). This test should be
// skipped, it is added for documenting purposes.
func TestAccountWithdrawalBenchmark(t *testing.T) {
	t.SkipNow()

	percentiles := []float64{80, 95, 98, 99, 99.7, 99.8, 99.9}

	// Configurations (#accounts,#withdrawals,#threads)
	configurations := [][]int{
		{100, 100000, 16},
		{100, 100000, 32},
		{100, 100000, 64},
		{100, 200000, 16},
		{100, 200000, 32},
		{100, 200000, 64},
		{100, 200000, 128},
		{100, 500000, 128},
		{100, 1000000, 16},
	}

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	var atomicWithdrawalErrs uint64
	for _, config := range configurations {
		acc, withdrawals, threads := config[0], config[1], config[2]

		// Log the configuration
		fmt.Printf("Configuration:\nAccounts: %d\nWithdrawals: %d\nThreads: %d\n\n", acc, withdrawals, threads)

		// Prepare the accounts
		accountIDs := make([]modules.AccountID, acc)
		accountSKs := make([]crypto.SecretKey, acc)
		accountBal := types.NewCurrency64(uint64(withdrawals))
		for a := 0; a < acc; a++ {
			sk, accountID := prepareAccount()
			accountIDs[a] = accountID
			accountSKs[a] = sk
			err = callDeposit(am, accountIDs[a], accountBal)
			if err != nil {
				t.Fatal(err)
			}
		}

		// Prepare withdrawals and signatures
		oneCurr := types.NewCurrency64(1)
		msgs := make([][]*modules.WithdrawalMessage, threads)
		sigs := make([][]crypto.Signature, threads)
		for t := 0; t < threads; t++ {
			msgs[t] = make([]*modules.WithdrawalMessage, withdrawals/threads)
			sigs[t] = make([]crypto.Signature, withdrawals/threads)
			for w := 0; w < withdrawals/threads; w++ {
				randIndex := fastrand.Intn(len(accountIDs))
				msgs[t][w], sigs[t][w] = prepareWithdrawal(accountIDs[randIndex], oneCurr, am.h.blockHeight, accountSKs[randIndex])
			}
		}

		// Run the withdrawals in separate threads
		var wg sync.WaitGroup
		timings := make([][]int64, threads)
		start := time.Now()
		for th := 0; th < threads; th++ {
			timings[th] = make([]int64, 0)
			wg.Add(1)
			go func(thread int) {
				defer wg.Done()
				for i := 0; i < withdrawals/threads; i++ {
					start := time.Now()
					if wErr := callWithdraw(am, msgs[thread][i], sigs[thread][i]); wErr != nil {
						atomic.AddUint64(&atomicWithdrawalErrs, 1)
						t.Log(wErr)
					}
					timeInMS := time.Since(start).Microseconds()
					timings[thread] = append(timings[thread], timeInMS)

					// Sleep to allow the GC some time. If we do not sleep here,
					// the percentile statistics are much worse due to outliers.
					time.Sleep(10 * time.Microsecond)
				}
			}(th)
		}
		wg.Wait()
		elapsed := time.Since(start)

		// Collect all timings in a single slice
		all := make([]int64, 0)
		for _, tpt := range timings {
			all = append(all, tpt...)
		}
		sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

		// Print the result
		for _, pc := range percentiles {
			atIndex := int(math.Round((pc/100)*float64(withdrawals+1))) - 1
			fmt.Printf("%v%% are below %vµs\n", pc, all[atIndex])
		}

		fmt.Printf("Finished in %vms\n\n\n", elapsed.Milliseconds())
		fmt.Println("- - - - - - - - - - - - - - - - - - - - - - - - ")
	}

	withdrawalErrors := atomic.LoadUint64(&atomicWithdrawalErrs)
	if withdrawalErrors != 0 {
		t.Fatal("Benchmarks can not be trusted on account of errors during the withdrawals")
	}
}

// TestAccountWithdrawalMultiple will deposit a large sum and make a lot of
// small withdrawals.
func TestAccountWithdrawalMultiple(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Grab some settings
	his := ht.host.InternalSettings()
	maxBalance := his.MaxEphemeralAccountBalance
	withdrawalSize := maxBalance.Div64(10000)

	// Note: withdrawals needs to be a multiple of threads for this test to pass
	withdrawals := 10000
	threads := 100

	// Prepare an account and fund it
	sk, accountID := prepareAccount()
	err = callDeposit(am, accountID, maxBalance)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare withdrawals and signatures
	msgs := make([]*modules.WithdrawalMessage, withdrawals)
	sigs := make([]crypto.Signature, withdrawals)
	for w := 0; w < int(withdrawals); w++ {
		msgs[w], sigs[w] = prepareWithdrawal(accountID, withdrawalSize, am.h.blockHeight, sk)
	}

	// Run the withdrawals in separate threads (ensure that withdrawals do not
	// exceed numDeposits * depositAmount)
	var wg sync.WaitGroup
	var atomicWithdrawalErrs uint64
	for th := 0; th < threads; th++ {
		wg.Add(1)
		go func(thread int) {
			defer wg.Done()
			for i := thread * (withdrawals / threads); i < (thread+1)*(withdrawals/threads); i++ {
				if wErr := callWithdraw(am, msgs[i], sigs[i]); wErr != nil {
					atomic.AddUint64(&atomicWithdrawalErrs, 1)
					t.Log(wErr)
				}
			}
		}(th)
	}
	wg.Wait()

	// Verify all withdrawals were successful
	withdrawalErrors := atomic.LoadUint64(&atomicWithdrawalErrs)
	if withdrawalErrors != 0 {
		t.Fatal("Unexpected error during withdrawals")
	}

	// Verify we've drained the account completely
	balance := getAccountBalance(am, accountID)
	if !balance.IsZero() {
		t.Log(balance.HumanString())
		t.Fatal("Unexpected account balance after withdrawals")
	}
}

// TestAccountWithdrawalBlockMultiple will deposit a large sum in increments,
// meanwhile making a lot of small withdrawals that will block but eventually
// resolve
func TestAccountWithdrawalBlockMultiple(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()

	// Deposit money into the account in small increments
	deposits := 20
	depositAmount := 50

	buckets := 10
	withdrawals := deposits * depositAmount
	withdrawalAmount := 1

	// Prepare withdrawals and signatures
	msgs := make([]*modules.WithdrawalMessage, withdrawals)
	sigs := make([]crypto.Signature, withdrawals)
	for w := 0; w < withdrawals; w++ {
		msgs[w], sigs[w] = prepareWithdrawal(accountID, types.NewCurrency64(uint64(withdrawalAmount)), am.h.blockHeight, sk)
	}

	// Add a waitgroup to wait for all deposits and withdrawals that are taking
	// concurrently taking place. Keep track of potential errors using atomics
	var wg sync.WaitGroup
	var atomicDepositErrs, atomicWithdrawalErrs uint64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for d := 0; d < deposits; d++ {
			time.Sleep(time.Duration(10 * time.Millisecond))
			if err := callDeposit(am, accountID, types.NewCurrency64(uint64(depositAmount))); err != nil {
				atomic.AddUint64(&atomicDepositErrs, 1)
			}
		}
	}()

	// Run the withdrawals in 10 separate buckets (ensure that withdrawals do
	// not exceed numDeposits * depositAmount)
	for b := 0; b < buckets; b++ {
		wg.Add(1)
		go func(bucket int) {
			defer wg.Done()
			for i := bucket * (withdrawals / buckets); i < (bucket+1)*(withdrawals/buckets); i++ {
				if wErr := callWithdraw(am, msgs[i], sigs[i]); wErr != nil {
					atomic.AddUint64(&atomicWithdrawalErrs, 1)
					t.Log(wErr)
				}
			}
		}(b)
	}
	wg.Wait()

	// Verify all deposits were successful
	depositErrors := atomic.LoadUint64(&atomicDepositErrs)
	if depositErrors != 0 {
		t.Fatal("Unexpected error during deposits")
	}

	// Verify all withdrawals were successful
	withdrawalErrors := atomic.LoadUint64(&atomicWithdrawalErrs)
	if withdrawalErrors != 0 {
		t.Fatal("Unexpected error during withdrawals")
	}

	// Account balance should be zero..
	balance := getAccountBalance(am, accountID)
	if !balance.IsZero() {
		t.Log(balance.String())
		t.Fatal("Unexpected account balance")
	}
}

// TestAccountMaxEphemeralAccountRisk tests the behaviour when the amount of
// unsaved ephemeral account balances exceeds the 'maxephemeralaccountrisk'. The
// account manager should wait until the asynchronous persist successfully
// completed before releasing the lock to accept more withdrawals.
func TestAccountMaxEphemeralAccountRisk(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host that persists the accounts data to disk with a certain
	// latency, this will ensure that we can reach the maxephemeralaccountrisk
	// and the host will effectively block until it drops below the maximum
	deps := dependencies.NewHostMaxEphemeralAccountRiskReached(200 * time.Millisecond)
	ht, err := blankMockHostTester(deps, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	his := ht.host.InternalSettings()
	maxRisk := his.MaxEphemeralAccountRisk
	maxBalance := his.MaxEphemeralAccountBalance

	// Use maxBalance in combination with maxRisk (and multiply by 2 to be sure)
	// to figure out a good amount of parallel accounts necessary to trigger
	// maxRisk to be reached.
	buckets, _ := maxRisk.Div(maxBalance).Mul64(2).Uint64()

	// Prepare the accounts
	accountSKs := make([]crypto.SecretKey, buckets)
	accountIDs := make([]modules.AccountID, buckets)
	for i := 0; i < int(buckets); i++ {
		sk, accountID := prepareAccount()
		accountSKs[i] = sk
		accountIDs[i] = accountID
	}

	// Fund all acounts to the max
	for _, acc := range accountIDs {
		if err = callDeposit(am, acc, maxBalance); err != nil {
			t.Fatal(err)
		}
	}

	cbh := am.h.blockHeight

	// Keep track of how many times the maxEpheramalAccountRisk was reached. We
	// assume that it works properly when this number exceeds 1, because this
	// means that it was also successful in decreasing the current risk when
	// the persist was successful
	var atomicMaxRiskReached uint64
	var wg sync.WaitGroup

	for i := 0; i < int(buckets); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			accID := accountIDs[i]
			accSK := accountSKs[i]
			msg, sig := prepareWithdrawal(accID, maxBalance, cbh, accSK)
			if wErr := callWithdraw(am, msg, sig); errors.Contains(wErr, errMaxRiskReached) {
				atomic.AddUint64(&atomicMaxRiskReached, 1)
			}
		}(i)
	}
	wg.Wait()

	if atomic.LoadUint64(&atomicMaxRiskReached) == 0 {
		t.Fatal("Max ephemeral account balance risk was not reached")
	}
}

// TestAccountIndexRecycling ensures that the account index of expired accounts
// properly recycle and are re-distributed among new accounts
func TestAccountIndexRecycling(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host & update its settings to expire accounts after 2s
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	hIS := ht.host.InternalSettings()
	hIS.EphemeralAccountExpiry = 2 * time.Second
	err = ht.host.SetInternalSettings(hIS)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	numAcc := 100
	accToIndex := make(map[modules.AccountID]uint32, numAcc)

	// deposit is a helper function to deposit 1H into the given account, this
	// acts as a keepalive for the account
	deposit := func(id modules.AccountID) {
		err := callDeposit(am, id, types.NewCurrency64(1))
		if err != nil {
			t.Fatal(err)
		}
	}

	// expire is a helper function that decides if an account should expire or
	// not. These indexes are deterministic but are random locations in the
	// 64 bit field.
	expire := func(id modules.AccountID) bool {
		index, ok := accToIndex[id]
		if !ok {
			t.Fatal("Unexpected failure, account id unknown")
		}
		return index > 0 && index%7 == 0
	}

	// Prepare a number of accounts
	for i := 0; i < numAcc; i++ {
		_, accountID := prepareAccount()
		deposit(accountID)
		persistInfo := am.managedAccountPersistInfo(accountID)
		if persistInfo == nil {
			t.Fatal("Unexpected failure, account id unknown")
		}
		accToIndex[accountID] = persistInfo.index
	}

	// Keep accounts alive past the expire frequency by periodically depositing
	// into the account
	doneChan := make(chan struct{})
	keepAliveFreq := time.Second
	go func() {
		for {
			select {
			case <-doneChan:
				return
			case <-time.After(keepAliveFreq):
				for id := range accToIndex {
					if !expire(id) {
						deposit(id)
					}
				}
			}
		}
	}()

	// provide ample time for accounts to expire
	time.Sleep(pruneExpiredAccountsFrequency * 2)

	// Verify that only accounts which have been inactive for longer than the
	// account expiry threshold are expired
	for id, index := range accToIndex {
		persistInfo := am.managedAccountPersistInfo(id)
		if expire(id) && persistInfo != nil {
			t.Logf("Expected account at index %d to be expired\n", index)
			t.Fatal("PruneExpiredAccount failure")
		} else if !expire(id) && persistInfo == nil {
			t.Logf("Expected account at index %d to be active\n", index)
			t.Fatal("PruneExpiredAccount failure")
		}
	}

	// For every expired index we want to create a new account, and verify that
	// the new account has recycled the index
	expired := make(map[uint32]bool)
	for id, index := range accToIndex {
		if expire(id) {
			expired[index] = true
			continue
		}
	}

	for i := len(expired); i > 0; i-- {
		_, accountID := prepareAccount()
		deposit(accountID)
		persistInfo := am.managedAccountPersistInfo(accountID)
		if persistInfo == nil {
			t.Fatal("Unexpected failure, account id unknown")
		}
		newIndex := persistInfo.index
		if _, ok := expired[newIndex]; !ok {
			t.Log(managedAccountIndexCheck(am))
			t.Fatalf("Account has index %v, instead of reusing a recycled index", newIndex)
		}
		delete(expired, newIndex)
	}

	doneChan <- struct{}{}
}

// TestAccountWithdrawalsInactive will test the account manager does not allow
// withdrawals when the host is not synced.
func TestAccountWithdrawalsInactive(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Prepare a host
	ht, err := blankHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	am := ht.host.staticAccountManager

	// Prepare an account
	sk, accountID := prepareAccount()
	oneCurrency := types.NewCurrency64(1)

	// Fund the account
	err = callDeposit(am, accountID, oneCurrency.Mul64(2))
	if err != nil {
		t.Fatal(err)
	}

	// Verify withdrawal is active
	msg, sig := prepareWithdrawal(accountID, oneCurrency, am.h.blockHeight, sk)
	err = callWithdraw(am, msg, sig)
	if err != nil {
		t.Fatal(err)
	}

	// Mock a consensus change that indicates the host is not synced
	am.callConsensusChanged(modules.ConsensusChange{Synced: false}, 0, 1)

	// Verify withdrawal is active
	msg, sig = prepareWithdrawal(accountID, oneCurrency, am.h.blockHeight, sk)
	err = callWithdraw(am, msg, sig)
	if !errors.Contains(err, modules.ErrWithdrawalsInactive) {
		t.Fatal(err)
	}
}

// managedCurrentRisk will return the current risk
func managedCurrentRisk(am *accountManager) types.Currency {
	am.mu.Lock()
	defer am.mu.Unlock()
	return am.currentRisk
}

// managedAccountIndexCheck is a sanity check performed on the account manager
// to detect duplicate index, this should never occur and is only used for
// testing/debugging purposes
func managedAccountIndexCheck(am *accountManager) string {
	am.mu.Lock()
	defer am.mu.Unlock()

	var msgs []string
	counts := make(map[uint32]int)
	for _, acc := range am.accounts {
		counts[acc.index]++
		if counts[acc.index] != 1 {
			msgs = append(msgs, fmt.Sprintf("duplicate index: %d", acc.index))
		}
	}

	if len(msgs) > 0 {
		return strings.Join(msgs, "\n")
	}
	return "No duplicate indexes found"
}

// callWithdraw will perform the withdrawal using a timestamp for the priority
func callWithdraw(am *accountManager, msg *modules.WithdrawalMessage, sig crypto.Signature) error {
	return am.callWithdraw(msg, sig, time.Now().UnixNano())
}

// callDeposit will perform the deposit on the account manager and close out the
// sync chan, which in production occurs when the file contract is fsynced to
// disk. To test that callDeposit can handle closed syncChans in a
// non-deterministic fashion the both are raced using a waitgroup and two
// goroutines.
func callDeposit(am *accountManager, id modules.AccountID, amount types.Currency) error {
	startChan := make(chan struct{})
	syncChan := make(chan struct{})

	var err error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-startChan
		err = am.callDeposit(id, amount, syncChan)
	}()
	go func() {
		defer wg.Done()
		<-startChan
		close(syncChan)
	}()
	close(startChan)
	wg.Wait()

	return err
}

// prepareWithdrawal prepares a withdrawal message, signs it using the provided
// secret key and returns the message and the signature
func prepareWithdrawal(id modules.AccountID, amount types.Currency, expiry types.BlockHeight, sk crypto.SecretKey) (*modules.WithdrawalMessage, crypto.Signature) {
	msg := &modules.WithdrawalMessage{
		Account: id,
		Expiry:  expiry,
		Amount:  amount,
	}
	copy(msg.Nonce[:], fastrand.Bytes(modules.WithdrawalNonceSize))

	hash := crypto.HashObject(*msg)
	sig := crypto.SignHash(hash, sk)
	return msg, sig
}

// prepareAccount will create an account and return its secret key alonside it's
// uplo public key
func prepareAccount() (_ crypto.SecretKey, aid modules.AccountID) {
	sk, pk := crypto.GenerateKeyPair()
	spk := types.UploPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	}
	aid.FromSPK(spk)
	return sk, aid
}

// randuint64 generates a random uint64
func randuint64() uint64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		build.Critical("could not generate random uint64")
	}
	return uint64(binary.LittleEndian.Uint64(b[:]))
}
