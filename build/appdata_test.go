package build

import (
	"os"
	"testing"
)

// TestAPIPassword tests getting and setting the API Password
func TestAPIPassword(t *testing.T) {
	// Unset any defaults, this only affects in memory state. Any Env Vars will
	// remain intact on disk
	err := os.Unsetenv(uploAPIPassword)
	if err != nil {
		t.Error(err)
	}

	// Calling APIPassword should return a non-blank password if the env
	// variable isn't set
	pw, err := APIPassword()
	if err != nil {
		t.Error(err)
	}
	if pw == "" {
		t.Error("Password should not be blank")
	}

	// Test setting the env variable
	newPW := "abc123"
	err = os.Setenv(uploAPIPassword, newPW)
	if err != nil {
		t.Error(err)
	}
	pw, err = APIPassword()
	if err != nil {
		t.Error(err)
	}
	if pw != newPW {
		t.Errorf("Expected password to be %v but was %v", newPW, pw)
	}
}

// TestuplodDataDir tests getting and setting the Uplo consensus directory
func TestuplodDataDir(t *testing.T) {
	// Unset any defaults, this only affects in memory state. Any Env Vars will
	// remain intact on disk
	err := os.Unsetenv(uplodDataDir)
	if err != nil {
		t.Error(err)
	}

	// Test Default uplodDataDir
	uplodDir := uplodDataDir()
	if uplodDir != "" {
		t.Errorf("Expected uplodDir to be empty but was %v", uplodDir)
	}

	// Test Env Variable
	newuplodir := "foo/bar"
	err = os.Setenv(uplodDataDir, newuplodir)
	if err != nil {
		t.Error(err)
	}
	uplodDir = uplodDataDir()
	if uplodDir != newuplodir {
		t.Errorf("Expected uplodDir to be %v but was %v", newuplodir, uplodDir)
	}
}

// Testuplodir tests getting and setting the Uplo data directory
func Testuplodir(t *testing.T) {
	// Unset any defaults, this only affects in memory state. Any Env Vars will
	// remain intact on disk
	err := os.Unsetenv(uplodataDir)
	if err != nil {
		t.Error(err)
	}

	// Test Default uplodir
	uplodir := uplodir()
	if uplodir != defaultuplodir() {
		t.Errorf("Expected uplodir to be %v but was %v", defaultuplodir(), uplodir)
	}

	// Test Env Variable
	newuplodir := "foo/bar"
	err = os.Setenv(uplodataDir, newuplodir)
	if err != nil {
		t.Error(err)
	}
	uplodir = uplodir()
	if uplodir != newuplodir {
		t.Errorf("Expected uplodir to be %v but was %v", newuplodir, uplodir)
	}
}

// TestUploWalletPassword tests getting and setting the Uplo Wallet Password
func TestUploWalletPassword(t *testing.T) {
	// Unset any defaults, this only affects in memory state. Any Env Vars will
	// remain intact on disk
	err := os.Unsetenv(uploWalletPassword)
	if err != nil {
		t.Error(err)
	}

	// Test Default Wallet Password
	pw := WalletPassword()
	if pw != "" {
		t.Errorf("Expected wallet password to be blank but was %v", pw)
	}

	// Test Env Variable
	newPW := "abc123"
	err = os.Setenv(uploWalletPassword, newPW)
	if err != nil {
		t.Error(err)
	}
	pw = WalletPassword()
	if pw != newPW {
		t.Errorf("Expected wallet password to be %v but was %v", newPW, pw)
	}
}

// TestUploExchangeRate tests getting and setting the Uplo Exchange Rate
func TestUploExchangeRate(t *testing.T) {
	// Unset any defaults, this only affects in memory state. Any Env Vars will
	// remain intact on disk
	err := os.Unsetenv(uploExchangeRate)
	if err != nil {
		t.Error(err)
	}

	// Test Default
	rate := ExchangeRate()
	if rate != "" {
		t.Errorf("Expected exchange rate to be blank but was %v", rate)
	}

	// Test Env Variable
	newRate := "abc123"
	err = os.Setenv(uploExchangeRate, newRate)
	if err != nil {
		t.Error(err)
	}
	rate = ExchangeRate()
	if rate != newRate {
		t.Errorf("Expected exchange rate to be %v but was %v", newRate, rate)
	}
}
