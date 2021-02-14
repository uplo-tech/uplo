package build

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/uplo-tech/errors"
)

var (
	// UploTestingDir is the directory that contains all of the files and
	// folders created during testing.
	UploTestingDir = filepath.Join(os.TempDir(), "UploTesting")
)

// TempDir joins the provided directories and prefixes them with the Uplo
// testing directory.
func TempDir(dirs ...string) string {
	path := filepath.Join(UploTestingDir, filepath.Join(dirs...))
	// remove old test data
	_ = os.RemoveAll(path) // ignore error instead of panicking in production
	return path
}

// CopyFile copies a file from a source to a destination.
func CopyFile(source, dest string) (err error) {
	sf, err := os.Open(source)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Compose(err, sf.Close())
	}()

	df, err := os.Create(dest)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Compose(err, df.Close())
	}()

	_, err = io.Copy(df, sf)
	return
}

// CopyDir copies a directory and all of its contents to the destination
// directory.
func CopyDir(source, dest string) error {
	stat, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return errors.New("source is not a directory")
	}

	err = os.MkdirAll(dest, stat.Mode())
	if err != nil {
		return err
	}
	files, err := ioutil.ReadDir(source)
	if err != nil {
		return err
	}
	for _, file := range files {
		newSource := filepath.Join(source, file.Name())
		newDest := filepath.Join(dest, file.Name())
		if file.IsDir() {
			err = CopyDir(newSource, newDest)
			if err != nil {
				return err
			}
		} else {
			err = CopyFile(newSource, newDest)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ExtractTarGz extracts the specified .tar.gz file to dir, overwriting
// existing files in the event of a name conflict.
func ExtractTarGz(filename, dir string) error {
	// Open the zipped archive.
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, file.Close())
	}()
	z, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, z.Close())
	}()
	t := tar.NewReader(z)

	// Create the output directory if it does not exist.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Read the file entries, writing each to dir.
	for {
		// Read header.
		hdr, err := t.Next()
		if errors.Contains(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		path := filepath.Join(dir, hdr.Name)
		info := hdr.FileInfo()
		if info.IsDir() {
			// Create directory.
			if err := os.MkdirAll(path, info.Mode()); err != nil {
				return err
			}
		} else {
			// Create file.
			tf, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
			if err != nil {
				return err
			}
			defer func() {
				err = errors.Compose(err, tf.Close())
			}()
			_, err = io.Copy(tf, t)
			if err != nil {
				return err
			}
		}
	}
}

// Retry will call 'fn' 'tries' times, waiting 'durationBetweenAttempts'
// between each attempt, returning 'nil' the first time that 'fn' returns nil.
// If 'nil' is never returned, then the final error returned by 'fn' is
// returned.
func Retry(tries int, durationBetweenAttempts time.Duration, fn func() error) (err error) {
	for i := 1; i < tries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		time.Sleep(durationBetweenAttempts)
	}
	return fn()
}
