package renter

// skyfile.go provides the tools for creating and uploading skyfiles, and then
// receiving the associated skylinks to recover the files. The skyfile is the
// fundamental data structure underpinning Skynet.
//
// The primary trick of the skyfile is that the initial data is stored entirely
// in a single sector which is put on the Uplo network using 1-of-N redundancy.
// Every replica has an identical Merkle root, meaning that someone attempting
// to fetch the file only needs the Merkle root and then some way to ask hosts
// on the network whether they have access to the Merkle root.
//
// That single sector then contains all of the other information that is
// necessary to recover the rest of the file. If the file is small enough, the
// entire file will be stored within the single sector. If the file is larger,
// the Merkle roots that are needed to download the remaining data get encoded
// into something called a 'fanout'. While the base chunk is required to use
// 1-of-N redundancy, the fanout chunks can use more sophisticated redundancy.
//
// The 1-of-N redundancy requirement really stems from the fact that Skylinks
// are only 34 bytes of raw data, meaning that there's only enough room in a
// Skylink to encode a single root. The fanout however has much more data to
// work with, meaning there is space to describe much fancier redundancy schemes
// and data fetching patterns.
//
// Skyfiles also contain some metadata which gets encoded as json. The
// intention is to allow uploaders to put any arbitrary metadata fields into
// their file and know that users will be able to see that metadata after
// downloading. A couple of fields such as the mode of the file are supported at
// the base level by Uplo.

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/fixtures"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"
	"github.com/uplo-tech/uplo/skykey"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
)

var (
	// SkyfileDefaultBaseChunkRedundancy establishes the default redundancy for
	// the base chunk of a skyfile.
	SkyfileDefaultBaseChunkRedundancy = build.Select(build.Var{
		Dev:      uint8(2),
		Standard: uint8(10),
		Testing:  uint8(2),
	}).(uint8)
)

var (
	// ErrEncryptionNotSupported is the error returned when Skykey encryption is
	// not supported for a Skynet action.
	ErrEncryptionNotSupported = errors.New("skykey encryption not supported")

	// ErrInvalidMetadata is the error returned when the metadata is not valid.
	ErrInvalidMetadata = errors.New("metadata is invalid")

	// ErrMetadataTooBig is the error returned when the metadata exceeds a
	// sectorsize.
	ErrMetadataTooBig = errors.New("metadata exceeds sectorsize")

	// ErrRedundancyNotSupported is the error returned when trying to convert a
	// Uplofile that was uploaded with redundancy that is not currently supported
	// by Skynet
	ErrRedundancyNotSupported = errors.New("skylinks currently only support 1-of-N redundancy, other redundancies will be supported in a later version")

	// ErrSkylinkBlocked is the error returned when a skylink is blocked
	ErrSkylinkBlocked = errors.New("skylink is blocked")
)

// skyfileEstablishDefaults will set any zero values in the lup to be equal to
// the desired defaults.
func skyfileEstablishDefaults(lup *modules.SkyfileUploadParameters) {
	if lup.BaseChunkRedundancy == 0 {
		lup.BaseChunkRedundancy = SkyfileDefaultBaseChunkRedundancy
	}
}

// fileUploadParams will create an erasure coder and return the FileUploadParams
// to use when uploading using the provided parameters.
func fileUploadParams(uploPath modules.UploPath, dataPieces, parityPieces int, force bool, ct crypto.CipherType) (modules.FileUploadParams, error) {
	// Create the erasure coder
	ec, err := modules.NewRSSubCode(dataPieces, parityPieces, crypto.SegmentSize)
	if err != nil {
		return modules.FileUploadParams{}, errors.AddContext(err, "unable to create erasure coder")
	}

	// Return the FileUploadParams
	return modules.FileUploadParams{
		UploPath:             uploPath,
		ErasureCode:         ec,
		Force:               force,
		DisablePartialChunk: true,  // must be set to true - partial chunks change, content addressed files must not change.
		Repair:              false, // indicates whether this is a repair operation
		CipherType:          ct,
	}, nil
}

// fileUploadParamsFromSUP will derive the FileUploadParams to use when
// uploading the base chunk uplofile of a skyfile using the skyfile's upload
// parameters.
func fileUploadParamsFromSUP(sup modules.SkyfileUploadParameters) (modules.FileUploadParams, error) {
	// Establish defaults
	skyfileEstablishDefaults(&sup)

	// Create parameters to upload the file with 1-of-N erasure coding and no
	// encryption. This should cause all of the pieces to have the same Merkle
	// root, which is critical to making the file discoverable to viewnodes and
	// also resilient to host failures.
	return fileUploadParams(sup.UploPath, 1, int(sup.BaseChunkRedundancy)-1, sup.Force, crypto.TypePlain)
}

// streamerFromReader wraps a bytes.Reader to give it a Close() method, which
// allows it to satisfy the modules.Streamer interface.
type streamerFromReader struct {
	*bytes.Reader
}

// Close is a no-op because a bytes.Reader doesn't need to be closed.
func (sfr *streamerFromReader) Close() error {
	return nil
}

// StreamerFromSlice returns a modules.Streamer given a slice. This is
// non-trivial because a bytes.Reader does not implement Close.
func StreamerFromSlice(b []byte) modules.Streamer {
	reader := bytes.NewReader(b)
	return &streamerFromReader{
		Reader: reader,
	}
}

// CreateSkylinkFromUplofile creates a skyfile from a uplofile. This requires
// uploading a new skyfile which contains fanout information pointing to the
// uplofile data. The UploPath provided in 'sup' indicates where the new base
// sector skyfile will be placed, and the uploPath provided as its own input is
// the uploPath of the file that is being used to create the skyfile.
func (r *Renter) CreateSkylinkFromUplofile(sup modules.SkyfileUploadParameters, uploPath modules.UploPath) (_ modules.Skylink, err error) {
	// Encryption is not supported for UploFile conversion.
	if encryptionEnabled(&sup) {
		return modules.Skylink{}, errors.AddContext(ErrEncryptionNotSupported, "unable to convert uplofile")
	}
	// Set reasonable default values for any sup fields that are blank.
	skyfileEstablishDefaults(&sup)

	// Grab the filenode for the provided uplopath.
	fileNode, err := r.staticFileSystem.OpenUploFile(uploPath)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to open uplofile")
	}
	defer func() {
		err = errors.Compose(err, fileNode.Close())
	}()

	// Override the metadata with the info from the fileNode.
	metadata := modules.SkyfileMetadata{
		Filename: uploPath.Name(),
		Mode:     fileNode.Mode(),
		Length:   fileNode.Size(),
	}
	return r.managedCreateSkylinkFromFileNode(sup, metadata, fileNode, nil)
}

// managedCreateSkylinkFromFileNode creates a skylink from a file node.
//
// The name needs to be passed in explicitly because a file node does not track
// its own name, which allows the file to be renamed concurrently without
// causing any race conditions.
func (r *Renter) managedCreateSkylinkFromFileNode(sup modules.SkyfileUploadParameters, skyfileMetadata modules.SkyfileMetadata, fileNode *filesystem.FileNode, fanoutReader io.Reader) (modules.Skylink, error) {
	// Check if the given metadata is valid
	err := modules.ValidateSkyfileMetadata(skyfileMetadata)
	if err != nil {
		return modules.Skylink{}, errors.Compose(ErrInvalidMetadata, err)
	}

	// Check if any of the skylinks associated with the uplofile are blocked
	if r.isFileNodeBlocked(fileNode) {
		// Skylink is blocked, return error and try and delete file
		return modules.Skylink{}, errors.Compose(ErrSkylinkBlocked, r.DeleteFile(sup.UploPath))
	}

	// Check that the encryption key and erasure code is compatible with the
	// skyfile format. This is intentionally done before any heavy computation
	// to catch errors early on.
	var sl modules.SkyfileLayout
	masterKey := fileNode.MasterKey()
	if len(masterKey.Key()) > len(sl.KeyData) {
		return modules.Skylink{}, errors.New("cipher key is not supported by the skyfile format")
	}
	ec := fileNode.ErasureCode()
	if ec.Type() != modules.ECReedSolomonSubShards64 {
		return modules.Skylink{}, errors.New("uplofile has unsupported erasure code type")
	}
	// Deny the conversion of uplofiles that are not 1 data piece. Not because we
	// cannot download them, but because it is currently inefficient to download
	// them.
	if ec.MinPieces() != 1 {
		return modules.Skylink{}, ErrRedundancyNotSupported
	}

	// Marshal the metadata.
	metadataBytes, err := modules.SkyfileMetadataBytes(skyfileMetadata)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "error retrieving skyfile metadata bytes")
	}

	// Create the fanout for the uplofile.
	fanoutBytes, err := skyfileEncodeFanout(fileNode, fanoutReader)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to encode the fanout of the uplofile")
	}
	headerSize := uint64(modules.SkyfileLayoutSize + len(metadataBytes) + len(fanoutBytes))
	if headerSize > modules.SectorSize {
		return modules.Skylink{}, errors.AddContext(ErrMetadataTooBig, fmt.Sprintf("skyfile does not fit in leading chunk - metadata size plus fanout size must be less than %v bytes, metadata size is %v bytes and fanout size is %v bytes", modules.SectorSize-modules.SkyfileLayoutSize, len(metadataBytes), len(fanoutBytes)))
	}

	// Assemble the first chunk of the skyfile.
	sl = modules.SkyfileLayout{
		Version:            modules.SkyfileVersion,
		Filesize:           fileNode.Size(),
		MetadataSize:       uint64(len(metadataBytes)),
		FanoutSize:         uint64(len(fanoutBytes)),
		FanoutDataPieces:   uint8(ec.MinPieces()),
		FanoutParityPieces: uint8(ec.NumPieces() - ec.MinPieces()),
		CipherType:         masterKey.Type(),
	}
	// If we're uploading in plaintext, we put the key in the baseSector
	if !encryptionEnabled(&sup) {
		copy(sl.KeyData[:], masterKey.Key())
	}

	// Create the base sector.
	baseSector, fetchSize := modules.BuildBaseSector(sl.Encode(), fanoutBytes, metadataBytes, nil)

	// Encrypt the base sector if necessary.
	if encryptionEnabled(&sup) {
		err = encryptBaseSectorWithSkykey(baseSector, sl, sup.FileSpecificSkykey)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "Failed to encrypt base sector for upload")
		}
	}

	// Create the skylink.
	baseSectorRoot := crypto.MerkleRoot(baseSector)
	skylink, err := modules.NewSkylinkV1(baseSectorRoot, 0, fetchSize)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to build skylink")
	}
	if sup.DryRun {
		return skylink, nil
	}

	// Check if the new skylink is blocked
	if r.staticSkynetBlocklist.IsBlocked(skylink) {
		// Skylink is blocked, return error and try and delete file
		return modules.Skylink{}, errors.Compose(ErrSkylinkBlocked, r.DeleteFile(sup.UploPath))
	}

	// Add the skylink to the uplofiles.
	err = fileNode.AddSkylink(skylink)
	if err != nil {
		return skylink, errors.AddContext(err, "unable to add skylink to the uplonodes")
	}

	// Upload the base sector.
	err = r.managedUploadBaseSector(sup, baseSector, skylink)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "Unable to upload base sector for file node. ")
	}

	return skylink, errors.AddContext(err, "unable to add skylink to the uplonodes")
}

// managedCreateFileNodeFromReader takes the file upload parameters and a reader
// and returns a filenode. This method turns the reader into a FileNode without
// effectively uploading the data. It is used to perform a dry-run of a skyfile
// upload.
func (r *Renter) managedCreateFileNodeFromReader(up modules.FileUploadParams, reader io.Reader) (*filesystem.FileNode, error) {
	// Check the upload params first.
	fileNode, err := r.managedInitUploadStream(up)
	if err != nil {
		return nil, err
	}

	// Extract some helper variables
	hpk := types.UploPublicKey{} // blank host key
	ec := fileNode.ErasureCode()
	psize := fileNode.PieceSize()
	csize := fileNode.ChunkSize()

	var peek []byte
	for chunkIndex := uint64(0); ; chunkIndex++ {
		// Grow the UploFile to the right size.
		err := fileNode.UploFile.GrowNumChunks(chunkIndex + 1)
		if err != nil {
			return nil, err
		}

		// Allocate data pieces and fill them with data from r.
		ss := NewStreamShard(reader, peek)
		err = func() (err error) {
			defer func() {
				err = errors.Compose(err, ss.Close())
			}()

			dataPieces, total, errRead := readDataPieces(ss, ec, psize)
			if errRead != nil {
				return errRead
			}

			dataEncoded, _ := ec.EncodeShards(dataPieces)
			for pieceIndex, dataPieceEnc := range dataEncoded {
				if err := fileNode.UploFile.AddPiece(hpk, chunkIndex, uint64(pieceIndex), crypto.MerkleRoot(dataPieceEnc)); err != nil {
					return err
				}
			}

			adjustedSize := fileNode.Size() - csize + total
			if err := fileNode.SetFileSize(adjustedSize); err != nil {
				return errors.AddContext(err, "failed to adjust FileSize")
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}

		_, err = ss.Result()
		if errors.Contains(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return fileNode, nil
}

// Blocklist returns the merkleroots that are on the blocklist
func (r *Renter) Blocklist() ([]crypto.Hash, error) {
	err := r.tg.Add()
	if err != nil {
		return []crypto.Hash{}, err
	}
	defer r.tg.Done()
	return r.staticSkynetBlocklist.Blocklist(), nil
}

// UpdateSkynetBlocklist updates the list of hashed merkleroots that are blocked
func (r *Renter) UpdateSkynetBlocklist(additions, removals []crypto.Hash) error {
	err := r.tg.Add()
	if err != nil {
		return err
	}
	defer r.tg.Done()
	return r.staticSkynetBlocklist.UpdateBlocklist(additions, removals)
}

// Portals returns the list of known skynet portals.
func (r *Renter) Portals() ([]modules.SkynetPortal, error) {
	err := r.tg.Add()
	if err != nil {
		return []modules.SkynetPortal{}, err
	}
	defer r.tg.Done()
	return r.staticSkynetPortals.Portals(), nil
}

// UpdateSkynetPortals updates the list of known Skynet portals that are listed.
func (r *Renter) UpdateSkynetPortals(additions []modules.SkynetPortal, removals []modules.NetAddress) error {
	err := r.tg.Add()
	if err != nil {
		return err
	}
	defer r.tg.Done()
	return r.staticSkynetPortals.UpdatePortals(additions, removals)
}

// managedUploadBaseSector will take the raw baseSector bytes and upload them,
// returning the resulting merkle root, and the fileNode of the uplofile that is
// tracking the base sector.
func (r *Renter) managedUploadBaseSector(lup modules.SkyfileUploadParameters, baseSector []byte, skylink modules.Skylink) (err error) {
	fileUploadParams, err := fileUploadParamsFromSUP(lup)
	if err != nil {
		return errors.AddContext(err, "failed to create uplofile upload parameters")
	}
	fileUploadParams.CipherType = crypto.TypePlain // the baseSector should be encrypted by the caller.

	// Perform the actual upload. This will require turning the base sector into
	// a reader.
	baseSectorReader := bytes.NewReader(baseSector)
	fileNode, err := r.callUploadStreamFromReader(fileUploadParams, baseSectorReader)
	if err != nil {
		return errors.AddContext(err, "failed to stream upload small skyfile")
	}
	defer func() {
		err = errors.Compose(err, fileNode.Close())
	}()

	// Add the skylink to the Uplofile.
	err = fileNode.AddSkylink(skylink)
	return errors.AddContext(err, "unable to add skylink to uplofile")
}

// managedUploadSkyfile uploads a file and returns the skylink and whether or
// not it was a large file.
func (r *Renter) managedUploadSkyfile(sup modules.SkyfileUploadParameters, reader modules.SkyfileUploadReader) (modules.Skylink, error) {
	// see if we can fit the entire upload in a single chunk
	buf := make([]byte, modules.SectorSize)
	numBytes, err := io.ReadFull(reader, buf)
	buf = buf[:numBytes] // truncate the buffer

	// if we've reached EOF, we can safely fetch the metadata and calculate the
	// actual header size, if that fits in a single sector we can upload the
	// Skyfile as a small file
	if errors.Contains(err, io.EOF) || errors.Contains(err, io.ErrUnexpectedEOF) {
		// get the skyfile metadata from the reader
		metadata, err := reader.SkyfileMetadata(r.tg.StopCtx())
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to get skyfile metadata")
		}

		// check whether it's valid
		err = modules.ValidateSkyfileMetadata(metadata)
		if err != nil {
			return modules.Skylink{}, errors.Compose(ErrInvalidMetadata, err)
		}
		// marshal the skyfile metadata into bytes
		metadataBytes, err := modules.SkyfileMetadataBytes(metadata)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to get skyfile metadata bytes")
		}

		// verify if it fits in a single chunk
		headerSize := uint64(modules.SkyfileLayoutSize + len(metadataBytes))
		if uint64(numBytes)+headerSize <= modules.SectorSize {
			return r.managedUploadSkyfileSmallFile(sup, metadataBytes, buf)
		}
	}

	// if we reach this point it means either we have not reached the EOF or the
	// data combined with the header exceeds a single sector, we add the data we
	// already read and upload as a large file
	reader.AddReadBuffer(buf)
	return r.managedUploadSkyfileLargeFile(sup, reader)
}

// managedUploadSkyfileSmallFile uploads a file that fits entirely in the
// leading chunk of a skyfile to the Uplo network and returns the skylink that
// can be used to access the file.
func (r *Renter) managedUploadSkyfileSmallFile(sup modules.SkyfileUploadParameters, metadataBytes, fileBytes []byte) (modules.Skylink, error) {
	sl := modules.SkyfileLayout{
		Version:      modules.SkyfileVersion,
		Filesize:     uint64(len(fileBytes)),
		MetadataSize: uint64(len(metadataBytes)),
		// No fanout is set yet.
		// If encryption is set in the upload params, this will be overwritten.
		CipherType: crypto.TypePlain,
	}

	// Create the base sector. This is done as late as possible so that any
	// errors are caught before a large block of memory is allocated.
	baseSector, fetchSize := modules.BuildBaseSector(sl.Encode(), nil, metadataBytes, fileBytes) // 'nil' because there is no fanout

	if encryptionEnabled(&sup) {
		err := encryptBaseSectorWithSkykey(baseSector, sl, sup.FileSpecificSkykey)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "Failed to encrypt base sector for upload")
		}
	}

	// Create the skylink.
	baseSectorRoot := crypto.MerkleRoot(baseSector) // Should be identical to the sector roots for each sector in the uplofile.
	skylink, err := modules.NewSkylinkV1(baseSectorRoot, 0, fetchSize)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "failed to build the skylink")
	}

	// If this is a dry-run, we do not need to upload the base sector
	if sup.DryRun {
		return skylink, nil
	}

	// Upload the base sector.
	err = r.managedUploadBaseSector(sup, baseSector, skylink)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "failed to upload base sector")
	}
	return skylink, nil
}

// managedUploadSkyfileLargeFile will accept a fileReader containing all of the
// data to a large uplofile and upload it to the Uplo network using
// 'callUploadStreamFromReader'. The final skylink is created by calling
// 'CreateSkylinkFromUplofile' on the resulting uplofile.
func (r *Renter) managedUploadSkyfileLargeFile(sup modules.SkyfileUploadParameters, fileReader modules.SkyfileUploadReader) (modules.Skylink, error) {
	// Create the uplopath for the skyfile extra data. This is going to be the
	// same as the skyfile upload uplopath, except with a suffix.
	uploPath, err := modules.NewUploPath(sup.UploPath.String() + modules.ExtendedSuffix)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create UploPath for large skyfile extended data")
	}
	// Create the FileUploadParams
	fup, err := fileUploadParams(uploPath, 1, int(sup.BaseChunkRedundancy)-1, sup.Force, crypto.TypePlain)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create FileUploadParams for large file")
	}

	// Generate a Cipher Key for the FileUploadParams.
	err = generateCipherKey(&fup, sup)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create Cipher key for FileUploadParams")
	}

	var fileNode *filesystem.FileNode
	if sup.DryRun {
		// In case of a dry-run we don't want to perform the actual upload,
		// instead we create a filenode that contains all of the data pieces and
		// their merkle roots.
		fileNode, err = r.managedCreateFileNodeFromReader(fup, fileReader)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to upload large skyfile")
		}
	} else {
		// Upload the file using a streamer.
		fileNode, err = r.callUploadStreamFromReader(fup, fileReader)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to upload large skyfile")
		}
	}

	// Defer closing the file
	defer func() {
		err := fileNode.Close()
		if err != nil {
			r.log.Printf("Could not close node, err: %s\n", err.Error())
		}
	}()

	// Get the SkyfileMetadata from the reader object.
	metadata, err := fileReader.SkyfileMetadata(r.tg.StopCtx())
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to get skyfile metadata")
	}

	// Convert the new uplofile we just uploaded into a skyfile using the
	// convert function.
	skylink, err := r.managedCreateSkylinkFromFileNode(sup, metadata, fileNode, fileReader.FanoutReader())
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create skylink from filenode")
	}
	return skylink, nil
}

// DownloadSkylink will take a link and turn it into the metadata and data of a
// download.
func (r *Renter) DownloadSkylink(link modules.Skylink, timeout time.Duration) (modules.SkyfileLayout, modules.SkyfileMetadata, modules.Streamer, error) {
	if err := r.tg.Add(); err != nil {
		return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, err
	}
	defer r.tg.Done()
	return r.managedDownloadSkylink(link, timeout)
}

// DownloadSkylinkBaseSector will take a link and turn it into the data of
// a basesector without any decoding of the metadata, fanout, or decryption.
func (r *Renter) DownloadSkylinkBaseSector(link modules.Skylink, timeout time.Duration) (modules.Streamer, error) {
	if err := r.tg.Add(); err != nil {
		return nil, err
	}
	defer r.tg.Done()
	baseSector, err := r.managedDownloadBaseSector(link, timeout)
	return StreamerFromSlice(baseSector), err
}

// managedDownloadSkylink will take a link and turn it into the metadata and
// data of a download.
func (r *Renter) managedDownloadSkylink(link modules.Skylink, timeout time.Duration) (modules.SkyfileLayout, modules.SkyfileMetadata, modules.Streamer, error) {
	if r.deps.Disrupt("resolveSkylinkToFixture") {
		sf, err := fixtures.LoadSkylinkFixture(link)
		if err != nil {
			return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, errors.AddContext(err, "failed to fetch fixture")
		}
		return modules.SkyfileLayout{}, sf.Metadata, StreamerFromSlice(sf.Content), nil
	}

	// Check if this skylink is already in the stream buffer set. If so, we can
	// skip the lookup procedure and use any data that other threads have
	// cached. Only do this if the skylink is not blocked. We still might have
	// cached, blocked data.
	if !r.staticSkynetBlocklist.IsBlocked(link) {
		id := link.DataSourceID()
		streamer, exists := r.staticStreamBufferSet.callNewStreamFromID(id, 0)
		if exists {
			return streamer.Layout(), streamer.Metadata(), streamer, nil
		}
	}

	// Try downloading the base sector.
	baseSector, err := r.managedDownloadBaseSector(link, timeout)
	if err != nil {
		return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, errors.AddContext(err, "unable to perform raw download of the skyfile")
	}

	// Check if the base sector is encrypted, and attempt to decrypt it.
	// This will fail if we don't have the decryption key.
	var fileSpecificSkykey skykey.Skykey
	if modules.IsEncryptedBaseSector(baseSector) {
		fileSpecificSkykey, err = r.decryptBaseSector(baseSector)
		if err != nil {
			return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, errors.AddContext(err, "Unable to decrypt skyfile base sector")
		}
	}

	// Parse out the metadata of the skyfile.
	layout, fanoutBytes, metadata, baseSectorPayload, err := modules.ParseSkyfileMetadata(baseSector)
	if err != nil {
		return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, errors.AddContext(err, "error parsing skyfile metadata")
	}

	// If there is no fanout, all of the data will be contained in the base
	// sector, return a streamer using the data from the base sector.
	if layout.FanoutSize == 0 {
		streamer := StreamerFromSlice(baseSectorPayload)
		return layout, metadata, streamer, nil
	}

	// There is a fanout, create a fanout streamer and return that.
	fs, err := r.newFanoutStreamer(link, layout, metadata, fanoutBytes, timeout, fileSpecificSkykey)
	if err != nil {
		return modules.SkyfileLayout{}, modules.SkyfileMetadata{}, nil, errors.AddContext(err, "unable to create fanout fetcher")
	}
	return layout, metadata, fs, nil
}

// managedDownloadBaseSector will download the baseSector for the skylink or
// return the active stream
func (r *Renter) managedDownloadBaseSector(link modules.Skylink, timeout time.Duration) ([]byte, error) {
	// Check if link is blocked
	if r.staticSkynetBlocklist.IsBlocked(link) {
		return nil, ErrSkylinkBlocked
	}

	// Pull the offset and fetchSize out of the skylink.
	offset, fetchSize, err := link.OffsetAndFetchSize()
	if err != nil {
		return nil, errors.AddContext(err, "unable to parse skylink")
	}

	// Fetch the leading chunk.
	baseSector, err := r.DownloadByRoot(link.MerkleRoot(), offset, fetchSize, timeout)
	if err != nil {
		return nil, errors.AddContext(err, "unable to fetch base sector of skylink")
	}
	if len(baseSector) < modules.SkyfileLayoutSize {
		return nil, errors.New("download did not fetch enough data, layout cannot be decoded")
	}

	// Return the baseSector
	return baseSector, nil
}

// PinSkylink will fetch the file associated with the Skylink, and then pin all
// necessary content to maintain that Skylink.
func (r *Renter) PinSkylink(skylink modules.Skylink, lup modules.SkyfileUploadParameters, timeout time.Duration) error {
	// Check if link is blocked
	if r.staticSkynetBlocklist.IsBlocked(skylink) {
		return ErrSkylinkBlocked
	}

	// Set sane defaults for unspecified values.
	skyfileEstablishDefaults(&lup)

	// Fetch the leading chunk.
	baseSector, err := r.DownloadByRoot(skylink.MerkleRoot(), 0, modules.SectorSize, timeout)
	if err != nil {
		return errors.AddContext(err, "unable to fetch base sector of skylink")
	}
	if uint64(len(baseSector)) != modules.SectorSize {
		return errors.New("download did not fetch enough data, file cannot be re-pinned")
	}

	// Check if the base sector is encrypted, and attempt to decrypt it.
	var fileSpecificSkykey skykey.Skykey
	encrypted := modules.IsEncryptedBaseSector(baseSector)
	if encrypted {
		fileSpecificSkykey, err = r.decryptBaseSector(baseSector)
		if err != nil {
			return errors.AddContext(err, "Unable to decrypt skyfile base sector")
		}
	}

	// Parse out the metadata of the skyfile.
	layout, fanoutBytes, metadata, _, err := modules.ParseSkyfileMetadata(baseSector)
	if err != nil {
		return errors.AddContext(err, "error parsing skyfile metadata")
	}

	// Start setting up the FUP.
	fup := modules.FileUploadParams{
		Force:               lup.Force,
		DisablePartialChunk: true,  // must be set to true - partial chunks change, content addressed files must not change.
		Repair:              false, // indicates whether this is a repair operation
		CipherType:          crypto.TypePlain,
	}

	// Re-encrypt the baseSector for upload and add the fanout key to the fup.
	if encrypted {
		err = encryptBaseSectorWithSkykey(baseSector, layout, fileSpecificSkykey)
		if err != nil {
			return errors.AddContext(err, "Error re-encrypting base sector")
		}

		// Derive the fanout key and add to the fup.
		fanoutSkykey, err := fileSpecificSkykey.DeriveSubkey(modules.FanoutNonceDerivation[:])
		if err != nil {
			return errors.AddContext(err, "Error deriving fanout skykey")
		}
		fup.CipherKey, err = fanoutSkykey.CipherKey()
		if err != nil {
			return errors.AddContext(err, "Error getting fanout CipherKey")
		}
		fup.CipherType = fanoutSkykey.CipherType()

		// These fields aren't used yet, but we'll set them anyway to mimic behavior in
		// upload/download code for consistency.
		lup.SkykeyName = fileSpecificSkykey.Name
		lup.FileSpecificSkykey = fileSpecificSkykey
	}

	// Re-upload the baseSector.
	err = r.managedUploadBaseSector(lup, baseSector, skylink)
	if err != nil {
		return errors.AddContext(err, "unable to upload base sector")
	}

	// If there is no fanout, nothing more to do, the pin is complete.
	if layout.FanoutSize == 0 {
		return nil
	}
	// Create the erasure coder to use when uploading the file bulk.
	fup.ErasureCode, err = modules.NewRSSubCode(int(layout.FanoutDataPieces), int(layout.FanoutParityPieces), crypto.SegmentSize)
	if err != nil {
		return errors.AddContext(err, "unable to create erasure coder for large file")
	}
	// Create the uplopath for the skyfile extra data. This is going to be the
	// same as the skyfile upload uplopath, except with a suffix.
	fup.UploPath, err = modules.NewUploPath(lup.UploPath.String() + modules.ExtendedSuffix)
	if err != nil {
		return errors.AddContext(err, "unable to create UploPath for large skyfile extended data")
	}

	// Create the fanout streamer that will download the file.
	streamer, err := r.newFanoutStreamer(skylink, layout, metadata, fanoutBytes, timeout, fileSpecificSkykey)
	if err != nil {
		return errors.AddContext(err, "Failed to create fanout streamer for large skyfile pin")
	}

	// Upload directly from the fanout download streamer.
	fileNode, err := r.callUploadStreamFromReader(fup, streamer)
	if err != nil {
		return errors.AddContext(err, "unable to upload large skyfile")
	}
	err = fileNode.AddSkylink(skylink)
	if err != nil {
		return errors.AddContext(err, "unable to upload skyfile fanout")
	}
	return nil
}

// RestoreSkyfile restores a skyfile from disk such that the skylink is
// preserved.
func (r *Renter) RestoreSkyfile(reader io.Reader) (modules.Skylink, error) {
	// Restore the skylink and baseSector from the reader
	skylinkStr, baseSector, err := modules.RestoreSkylink(reader)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to restore skyfile from backup")
	}

	// Load the skylink
	var skylink modules.Skylink
	err = skylink.LoadString(skylinkStr)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to load skylink")
	}

	// Check if the new skylink is blocked
	if r.staticSkynetBlocklist.IsBlocked(skylink) {
		return modules.Skylink{}, ErrSkylinkBlocked
	}

	// Check if the base sector is encrypted, and attempt to decrypt it.
	// This will fail if we don't have the decryption key.
	var fileSpecificSkykey skykey.Skykey
	encrypted := modules.IsEncryptedBaseSector(baseSector)
	if encrypted {
		fileSpecificSkykey, err = r.decryptBaseSector(baseSector)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "Unable to decrypt skyfile base sector")
		}
	}

	// Parse the baseSector.
	sl, _, sm, _, err := modules.ParseSkyfileMetadata(baseSector)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "error parsing the baseSector")
	}

	// Create the upload parameters
	uploPath, err := modules.SkynetFolder.Join(skylinkStr)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create uplopath")
	}
	sup := modules.SkyfileUploadParameters{
		BaseChunkRedundancy: sl.FanoutDataPieces + sl.FanoutParityPieces,
		UploPath:             uploPath,

		// Set filename and mode
		Filename: sm.Filename,
		Mode:     sm.Mode,

		// Set the default path params
		DefaultPath:        sm.DefaultPath,
		DisableDefaultPath: sm.DisableDefaultPath,
	}
	skyfileEstablishDefaults(&sup)

	// Re-encrypt the baseSector for upload and set the Skykey fields of the sup.
	if encrypted {
		err = encryptBaseSectorWithSkykey(baseSector, sl, fileSpecificSkykey)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "error re-encrypting base sector")
		}

		// Set the Skykey fields
		sup.SkykeyName = fileSpecificSkykey.Name
		sup.FileSpecificSkykey = fileSpecificSkykey
	}

	// Create the SkyfileUploadReader for the restoration
	var restoreReader modules.SkyfileUploadReader
	var buf bytes.Buffer
	// Define a TeeReader for the underlying io.Reader. This allows the fanout
	// bytes to be generated before the upload has completed by reading the data
	// from the buffer rather than the chunks.
	tee := io.TeeReader(reader, &buf)
	if len(sm.Subfiles) == 0 {
		restoreReader = modules.NewSkyfileReader(tee, sup)
	} else {
		// Create multipart reader from the subfiles
		multiReader, err := modules.NewMultipartReader(tee, sm.Subfiles)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to create multireader")
		}
		// Create the multipart reader for the fanout using the TeeReader's buffer.
		multiReaderFanout, err := modules.NewMultipartReader(&buf, sm.Subfiles)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to create multireader")
		}
		restoreReader = modules.NewSkyfileMultipartReader(multiReader, multiReaderFanout, sup)
	}

	// Upload the Base Sector of the skyfile
	err = r.managedUploadBaseSector(sup, baseSector, skylink)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "failed to upload base sector")
	}

	// If there was no fanout then we are done.
	if sl.FanoutSize == 0 {
		return skylink, nil
	}

	// Create erasure coder and FileUploadParams
	extendedPath, err := modules.NewUploPath(sup.UploPath.String() + modules.ExtendedSuffix)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create extended uplopath")
	}

	// Create the FileUploadParams
	fup, err := fileUploadParams(extendedPath, int(sl.FanoutDataPieces), int(sl.FanoutParityPieces), sup.Force, sl.CipherType)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to create FileUploadParams for large file")
	}

	// Generate a Cipher Key for the FileUploadParams.
	//
	// NOTE: Specifically using TypeThreefish instead of TypeDefaultRenter for two
	// reason. First, TypeThreefish was the CipherType of the uplofiles when
	// Skyfiles were introduced. Second, this should make the tests fail if the
	// TypeDefaultRenter changes, ensuring we add compat code for older converted
	// uplofiles.
	if sl.CipherType == crypto.TypeThreefish {
		// For converted files we need to generate a UploKey
		fup.CipherKey, err = crypto.NewUploKey(sl.CipherType, sl.KeyData[:])
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to create Cipher key from SkyfileLayout KeyData")
		}
	} else {
		err = generateCipherKey(&fup, sup)
		if err != nil {
			return modules.Skylink{}, errors.AddContext(err, "unable to create Cipher key for FileUploadParams")
		}
	}

	// Upload the file
	fileNode, err := r.callUploadStreamFromReader(fup, restoreReader)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to upload large skyfile")
	}

	// Defer closing the file
	defer func() {
		if err := fileNode.Close(); err != nil {
			r.log.Printf("Could not close node, err: %s\n", err.Error())
		}
	}()

	// Check if any of the skylinks associated with the uplofile are blocked
	if r.isFileNodeBlocked(fileNode) {
		// Skylink is blocked, return error and try and delete file
		return modules.Skylink{}, errors.Compose(ErrSkylinkBlocked, r.DeleteFile(sup.UploPath))
	}

	// Add the skylink to the uplofiles.
	err = fileNode.AddSkylink(skylink)
	if err != nil {
		err = errors.AddContext(err, "unable to add skylink to the uplonodes")
		return modules.Skylink{}, errors.Compose(err, r.DeleteFile(sup.UploPath))
	}

	return skylink, nil
}

// UploadSkyfile will upload the provided data with the provided metadata,
// returning a skylink which can be used by any portal to recover the full
// original file and metadata. The skylink will be unique to the combination of
// both the file data and metadata.
func (r *Renter) UploadSkyfile(sup modules.SkyfileUploadParameters, reader modules.SkyfileUploadReader) (skylink modules.Skylink, err error) {
	// Set reasonable default values for any sup fields that are blank.
	skyfileEstablishDefaults(&sup)

	// If a skykey name or ID was specified, generate a file-specific key for
	// this upload.
	err = r.generateFilekey(&sup, nil)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to upload skyfile")
	}

	// defer a function that cleans up the uplofiles after a failed upload
	// attempt or after a dry run
	defer func() {
		if err != nil || sup.DryRun {
			if err := r.DeleteFile(sup.UploPath); err != nil && !errors.Contains(err, filesystem.ErrNotExist) {
				r.log.Printf("error deleting uplofile after upload error: %v", err)
			}

			extendedPath := sup.UploPath.String() + modules.ExtendedSuffix
			extendedUploPath, _ := modules.NewUploPath(extendedPath)
			if err := r.DeleteFile(extendedUploPath); err != nil && !errors.Contains(err, filesystem.ErrNotExist) {
				r.log.Printf("error deleting extended uplofile after upload error: %v\n", err)
			}
		}
	}()

	// Upload the skyfile
	skylink, err = r.managedUploadSkyfile(sup, reader)
	if err != nil {
		return modules.Skylink{}, errors.AddContext(err, "unable to upload skyfile")
	}
	if r.deps.Disrupt("SkyfileUploadFail") {
		return modules.Skylink{}, errors.New("SkyfileUploadFail")
	}

	// Check if skylink is blocked
	if r.staticSkynetBlocklist.IsBlocked(skylink) && !sup.DryRun {
		return modules.Skylink{}, ErrSkylinkBlocked
	}

	return skylink, nil
}

// isFileNodeBlocked checks if any of the skylinks associated with the uplofile
// are blocked
func (r *Renter) isFileNodeBlocked(fileNode *filesystem.FileNode) bool {
	skylinkstrs := fileNode.Metadata().Skylinks
	for _, skylinkstr := range skylinkstrs {
		var skylink modules.Skylink
		err := skylink.LoadString(skylinkstr)
		if err != nil {
			// If there is an error just continue as we shouldn't prevent the
			// conversion due to bad old skylinks
			//
			// Log the error for debugging purposes
			r.log.Printf("WARN: previous skylink for uplofile %v could not be loaded from string; potentially corrupt skylink: %v", fileNode.UploFilePath(), skylinkstr)
			continue
		}
		// Check if skylink is blocked
		if r.staticSkynetBlocklist.IsBlocked(skylink) {
			return true
		}
	}
	return false
}
