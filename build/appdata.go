package build

import (
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/uplo-tech/fastrand"
)

// APIPassword returns the Uplo API Password either from the environment variable
// or from the password file. If no environment variable is set and no file
// exists, a password file is created and that password is returned
func APIPassword() (string, error) {
	// Check the environment variable.
	pw := os.Getenv(uploAPIPassword)
	if pw != "" {
		return pw, nil
	}

	// Try to read the password from disk.
	path := apiPasswordFilePath()
	pwFile, err := ioutil.ReadFile(path)
	if err == nil {
		// This is the "normal" case, so don't print anything.
		return strings.TrimSpace(string(pwFile)), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// No password file; generate a secure one.
	// Generate a password file.
	pw, err = createAPIPasswordFile()
	if err != nil {
		return "", err
	}
	return pw, nil
}

// ProfileDir returns the directory where any profiles for the running uplod
// instance will be stored
func ProfileDir() string {
	return filepath.Join(uplodDataDir(), "profile")
}

// uplodDataDir returns the uplod consensus data directory from the
// environment variable. If there is no environment variable it returns an empty
// string, instructing uplod to store the consensus in the current directory.
func uplodDataDir() string {
	return os.Getenv(uplodDataDir)
}

// uplodir returns the Uplo data directory either from the environment variable or
// the default.
func uplodir() string {
	uplodir := os.Getenv(uplodataDir)
	if uplodir == "" {
		uplodir = defaultuplodir()
	}
	return uplodir
}

// SkynetDir returns the Skynet data directory.
func SkynetDir() string {
	return defaultSkynetDir()
}

// WalletPassword returns the UploWalletPassword environment variable.
func WalletPassword() string {
	return os.Getenv(uploWalletPassword)
}

// ExchangeRate returns the uploExchangeRate environment variable.
func ExchangeRate() string {
	return os.Getenv(uploExchangeRate)
}

// apiPasswordFilePath returns the path to the API's password file. The password
// file is stored in the Uplo data directory.
func apiPasswordFilePath() string {
	return filepath.Join(uplodir(), "apipassword")
}

// createAPIPasswordFile creates an api password file in the Uplo data directory
// and returns the newly created password
func createAPIPasswordFile() (string, error) {
	err := os.MkdirAll(uplodir(), 0700)
	if err != nil {
		return "", err
	}
	// Ensure uplodir has the correct mode as MkdirAll won't change the mode of
	// an existent directory. We specifically use 0700 in order to prevent
	// potential attackers from accessing the sensitive information inside, both
	// by reading the contents of the directory and/or by creating files with
	// specific names which uplod would later on read from and/or write to.
	err = os.Chmod(uplodir(), 0700)
	if err != nil {
		return "", err
	}
	pw := hex.EncodeToString(fastrand.Bytes(16))
	err = ioutil.WriteFile(apiPasswordFilePath(), []byte(pw+"\n"), 0600)
	if err != nil {
		return "", err
	}
	return pw, nil
}

// defaultuplodir returns the default data directory of uplod. The values for
// supported operating systems are:
//
// Linux:   $HOME/.uplo
// MacOS:   $HOME/Library/Application Support/Uplo
// Windows: %LOCALAPPDATA%\Uplo
func defaultuplodir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Uplo")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Uplo")
	default:
		return filepath.Join(os.Getenv("HOME"), ".uplo")
	}
}

// defaultSkynetDir returns default data directory for miscellaneous Skynet data,
// e.g. skykeys. The values for supported operating systems are:
//
// Linux:   $HOME/.skynet
// MacOS:   $HOME/Library/Application Support/Skynet
// Windows: %LOCALAPPDATA%\Skynet
func defaultSkynetDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Skynet")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Skynet")
	default:
		return filepath.Join(os.Getenv("HOME"), ".skynet")
	}
}
