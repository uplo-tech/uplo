package api

import (
	"archive/tar"
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/skykey"
	"github.com/uplo-tech/errors"
)

type (
	// skyfileUploadParams is a helper struct that contains all of the query
	// string parameters on upload
	skyfileUploadParams struct {
		baseChunkRedundancy uint8
		defaultPath         string
		convertPath         string
		disableDefaultPath  bool
		dryRun              bool
		filename            string
		force               bool
		mode                os.FileMode
		root                bool
		uploPath             modules.UploPath
		skyKeyID            skykey.SkykeyID
		skyKeyName          string
	}

	// skyfileUploadHeaders is a helper struct that contains all of the request
	// headers on upload
	skyfileUploadHeaders struct {
		mediaType    string
		disableForce bool
	}
)

// buildETag is a helper function that returns an ETag.
func buildETag(skylink modules.Skylink, method, path string, format modules.SkyfileFormat) string {
	return crypto.HashAll(
		skylink.String(),
		method,
		path,
		string(format),
	).String()
}

// isMultipartRequest is a helper method that checks if the given media type
// matches that of a multipart form.
func isMultipartRequest(mediaType string) bool {
	return strings.HasPrefix(mediaType, "multipart/form-data")
}

// parseSkylinkURL splits a raw skylink URL into its components - a skylink, a
// string representation of the skylink with the query parameters stripped, and
// a path. The input skylink URL should not have been URL-decoded. The path is
// URL-decoded before returning as it is for us to parse and use, while the
// other components remain encoded for the skapp.
func parseSkylinkURL(skylinkURL, apiRoute string) (skylink modules.Skylink, skylinkStringNoQuery, path string, err error) {
	s := strings.TrimPrefix(skylinkURL, apiRoute)
	s = strings.TrimPrefix(s, "/")
	// Parse out optional path to a subfile
	path = "/" // default to root
	splits := strings.SplitN(s, "?", 2)
	skylinkStringNoQuery = splits[0]
	splits = strings.SplitN(skylinkStringNoQuery, "/", 2)
	// Check if a path is passed.
	if len(splits) > 1 && len(splits[1]) > 0 {
		path = modules.EnsurePrefix(splits[1], "/")
	}
	// Decode the path as it may contain URL-encoded characters.
	path, err = url.QueryUnescape(path)
	if err != nil {
		return
	}
	// Parse skylink
	err = skylink.LoadString(s)
	return
}

// parseTimeout tries to parse the timeout from the query string and validate
// it. If not present, it will default to DefaultSkynetRequestTimeout.
func parseTimeout(queryForm url.Values) (time.Duration, error) {
	timeoutStr := queryForm.Get("timeout")
	if timeoutStr == "" {
		return DefaultSkynetRequestTimeout, nil
	}

	timeoutInt, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return 0, errors.AddContext(err, "unable to parse 'timeout'")
	}
	if timeoutInt > MaxSkynetRequestTimeout {
		return 0, errors.AddContext(err, fmt.Sprintf("'timeout' parameter too high, maximum allowed timeout is %ds", MaxSkynetRequestTimeout))
	}
	return time.Duration(timeoutInt) * time.Second, nil
}

// parseUploadHeadersAndRequestParameters is a helper function that parses all
// of the query parameters and headers from an upload request
func parseUploadHeadersAndRequestParameters(req *http.Request, ps httprouter.Params) (*skyfileUploadHeaders, *skyfileUploadParams, error) {
	var err error

	// parse 'Skynet-Disable-Force' request header
	var disableForce bool
	strDisableForce := req.Header.Get("Skynet-Disable-Force")
	if strDisableForce != "" {
		disableForce, err = strconv.ParseBool(strDisableForce)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'Skynet-Disable-Force' header")
		}
	}

	// parse 'Content-Type' request header
	ct := req.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, nil, errors.AddContext(err, "failed parsing 'Content-Type' header")
	}

	// parse query
	queryForm, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return nil, nil, errors.AddContext(err, "failed to parse query")
	}

	// parse 'basechunkredundancy' query parameter
	baseChunkRedundancy := uint8(0)
	if rStr := queryForm.Get("basechunkredundancy"); rStr != "" {
		if _, err := fmt.Sscan(rStr, &baseChunkRedundancy); err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'basechunkredundancy' parameter")
		}
	}

	// parse 'convertpath' query parameter
	convertPath := queryForm.Get("convertpath")

	// parse 'defaultpath' query parameter
	defaultPath := queryForm.Get("defaultpath")
	if defaultPath != "" {
		defaultPath = modules.EnsurePrefix(defaultPath, "/")
	}

	// parse 'disabledefaultpath' query parameter
	var disableDefaultPath bool
	disableDefaultPathStr := queryForm.Get("disabledefaultpath")
	if disableDefaultPathStr != "" {
		disableDefaultPath, err = strconv.ParseBool(disableDefaultPathStr)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'disabledefaultpath' parameter")
		}
	}

	// parse 'dryrun' query parameter
	var dryRun bool
	dryRunStr := queryForm.Get("dryrun")
	if dryRunStr != "" {
		dryRun, err = strconv.ParseBool(dryRunStr)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'dryrun' parameter")
		}
	}

	// parse 'filename' query parameter
	filename := queryForm.Get("filename")

	// parse 'force' query parameter
	var force bool
	strForce := queryForm.Get("force")
	if strForce != "" {
		force, err = strconv.ParseBool(strForce)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'force' parameter")
		}
	}

	// parse 'mode' query parameter
	modeStr := queryForm.Get("mode")
	var mode os.FileMode
	if modeStr != "" {
		_, err := fmt.Sscanf(modeStr, "%o", &mode)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'mode' parameter")
		}
	}

	// parse 'root' query parameter
	var root bool
	rootStr := queryForm.Get("root")
	if rootStr != "" {
		root, err = strconv.ParseBool(rootStr)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'root' parameter")
		}
	}

	// parse 'uplopath' query parameter
	var uploPath modules.UploPath
	uploPathStr := ps.ByName("uplopath")
	if root {
		uploPath, err = modules.NewUploPath(uploPathStr)
	} else {
		uploPath, err = modules.SkynetFolder.Join(uploPathStr)
	}
	if err != nil {
		return nil, nil, errors.AddContext(err, "unable to parse 'uplopath' parameter")
	}

	// parse 'skykeyname' query parameter
	skykeyName := queryForm.Get("skykeyname")

	// parse 'skykeyid' query parameter
	var skykeyID skykey.SkykeyID
	skykeyIDStr := queryForm.Get("skykeyid")
	if skykeyIDStr != "" {
		err = skykeyID.FromString(skykeyIDStr)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to parse 'skykeyid'")
		}
	}

	// validate parameter combos

	// verify force is not set if disable force header was set
	if disableForce && force {
		return nil, nil, errors.New("'force' has been disabled on this node")
	}

	// verify the dry-run and force parameter are not combined
	if !disableForce && force && dryRun {
		return nil, nil, errors.New("'dryRun' and 'force' can not be combined")
	}

	// verify disabledefaultpath and defaultpath are not combined
	if disableDefaultPath && defaultPath != "" {
		return nil, nil, errors.AddContext(modules.ErrInvalidDefaultPath, "DefaultPath and DisableDefaultPath are mutually exclusive and cannot be set together")
	}

	// verify default path params are not set if it's not a multipart upload
	if !isMultipartRequest(mediaType) && (disableDefaultPath || defaultPath != "") {
		return nil, nil, errors.New("DefaultPath and DisableDefaultPath can only be set on multipart uploads")
	}

	// verify convertpath and filename are not combined
	if convertPath != "" && filename != "" {
		return nil, nil, errors.New("cannot set both a 'convertpath' and a 'filename'")
	}

	// verify skykeyname and skykeyid are not combined
	if skykeyName != "" && skykeyIDStr != "" {
		return nil, nil, errors.New("cannot set both a 'skykeyname' and 'skykeyid'")
	}

	// create headers and parameters
	headers := &skyfileUploadHeaders{
		disableForce: disableForce,
		mediaType:    mediaType,
	}
	params := &skyfileUploadParams{
		baseChunkRedundancy: baseChunkRedundancy,
		convertPath:         convertPath,
		defaultPath:         defaultPath,
		disableDefaultPath:  disableDefaultPath,
		dryRun:              dryRun,
		filename:            filename,
		force:               force,
		mode:                mode,
		root:                root,
		uploPath:             uploPath,
		skyKeyID:            skykeyID,
		skyKeyName:          skykeyName,
	}
	return headers, params, nil
}

// serveArchive serves skyfiles as an archive by reading them from r and writing
// the archive to dst using the given archiveFunc.
func serveArchive(dst io.Writer, src io.ReadSeeker, md modules.SkyfileMetadata, archiveFunc archiveFunc) error {
	// Get the files to archive.
	var files []modules.SkyfileSubfileMetadata
	for _, file := range md.Subfiles {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Offset < files[j].Offset
	})
	// If there are no files, it's a single file download. Manually construct a
	// SkyfileSubfileMetadata from the SkyfileMetadata.
	if len(files) == 0 {
		length := md.Length
		if md.Length == 0 {
			// v150Compat a missing length is fine for legacy links but new
			// links should always have the length set.
			if build.Release == "testing" {
				build.Critical("SkyfileMetadata is missing length")
			}
			// Fetch the length of the file by seeking to the end and then back
			// to the start.
			seekLen, err := src.Seek(0, io.SeekEnd)
			if err != nil {
				return errors.AddContext(err, "serveArchive: failed to seek to end of skyfile")
			}
			_, err = src.Seek(0, io.SeekStart)
			if err != nil {
				return errors.AddContext(err, "serveArchive: failed to seek to start of skyfile")
			}
			length = uint64(seekLen)
		}
		// Construct the SkyfileSubfileMetadata.
		files = append(files, modules.SkyfileSubfileMetadata{
			FileMode: md.Mode,
			Filename: md.Filename,
			Offset:   0,
			Len:      length,
		})
	}
	return archiveFunc(dst, src, files)
}

// serveTar is an archiveFunc that implements serving the files from src to dst
// as a tar.
func serveTar(dst io.Writer, src io.Reader, files []modules.SkyfileSubfileMetadata) error {
	tw := tar.NewWriter(dst)
	for _, file := range files {
		// Create header.
		header, err := tar.FileInfoHeader(file, file.Name())
		if err != nil {
			return err
		}
		// Modify name to match path within skyfile.
		header.Name = file.Filename
		// Write header.
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// Write file content.
		if _, err := io.CopyN(tw, src, header.Size); err != nil {
			return err
		}
	}
	return tw.Close()
}

// serveZip is an archiveFunc that implements serving the files from src to dst
// as a zip.
func serveZip(dst io.Writer, src io.Reader, files []modules.SkyfileSubfileMetadata) error {
	zw := zip.NewWriter(dst)
	for _, file := range files {
		f, err := zw.Create(file.Filename)
		if err != nil {
			return errors.AddContext(err, "serveZip: failed to add the file to the zip")
		}

		// Write file content.
		_, err = io.CopyN(f, src, int64(file.Len))
		if err != nil {
			return errors.AddContext(err, "serveZip: failed to write file contents to the zip")
		}
	}
	return zw.Close()
}
