package proto

import (
	"crypto/cipher"
	"net"
	"time"

	"github.com/uplo-tech/errors"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// extendDeadline is a helper function for extending the connection timeout.
func extendDeadline(conn net.Conn, d time.Duration) { _ = conn.SetDeadline(time.Now().Add(d)) }

// startRevision is run at the beginning of each revision iteration. It reads
// the host's settings confirms that the values are acceptable, and writes an acceptance.
func startRevision(conn net.Conn, host modules.HostDBEntry) error {
	// verify the host's settings and confirm its identity
	_, err := verifySettings(conn, host)
	if err != nil {
		return err
	}
	return modules.WriteNegotiationAcceptance(conn)
}

// startDownload is run at the beginning of each download iteration. It reads
// the host's settings confirms that the values are acceptable, and writes an acceptance.
func startDownload(conn net.Conn, host modules.HostDBEntry) error {
	// verify the host's settings and confirm its identity
	_, err := verifySettings(conn, host)
	if err != nil {
		return err
	}
	return modules.WriteNegotiationAcceptance(conn)
}

// verifySettings reads a signed HostSettings object from conn, validates the
// signature, and checks for discrepancies between the known settings and the
// received settings. If there is a discrepancy, the hostDB is notified. The
// received settings are returned.
func verifySettings(conn net.Conn, host modules.HostDBEntry) (modules.HostDBEntry, error) {
	// convert host key (types.UploPublicKey) to a crypto.PublicKey
	if host.PublicKey.Algorithm != types.SignatureEd25519 || len(host.PublicKey.Key) != crypto.PublicKeySize {
		build.Critical("hostdb did not filter out host with wrong signature algorithm:", host.PublicKey.Algorithm)
		return modules.HostDBEntry{}, errors.New("host used unsupported signature algorithm")
	}
	var pk crypto.PublicKey
	copy(pk[:], host.PublicKey.Key)

	// read signed host settings
	var recvSettings modules.HostOldExternalSettings
	if err := crypto.ReadSignedObject(conn, &recvSettings, modules.NegotiateMaxHostExternalSettingsLen, pk); err != nil {
		return modules.HostDBEntry{}, errors.New("couldn't read host's settings: " + err.Error())
	}
	// TODO: check recvSettings against host.HostExternalSettings. If there is
	// a discrepancy, write the error to conn.
	if recvSettings.NetAddress != host.NetAddress {
		// for now, just overwrite the NetAddress, since we know that
		// host.NetAddress works (it was the one we dialed to get conn)
		recvSettings.NetAddress = host.NetAddress
	}
	host.HostExternalSettings = modules.HostExternalSettings{
		AcceptingContracts:     recvSettings.AcceptingContracts,
		MaxDownloadBatchSize:   recvSettings.MaxDownloadBatchSize,
		MaxDuration:            recvSettings.MaxDuration,
		MaxReviseBatchSize:     recvSettings.MaxReviseBatchSize,
		NetAddress:             recvSettings.NetAddress,
		RemainingStorage:       recvSettings.RemainingStorage,
		SectorSize:             recvSettings.SectorSize,
		TotalStorage:           recvSettings.TotalStorage,
		UnlockHash:             recvSettings.UnlockHash,
		WindowSize:             recvSettings.WindowSize,
		Collateral:             recvSettings.Collateral,
		MaxCollateral:          recvSettings.MaxCollateral,
		ContractPrice:          recvSettings.ContractPrice,
		DownloadBandwidthPrice: recvSettings.DownloadBandwidthPrice,
		StoragePrice:           recvSettings.StoragePrice,
		UploadBandwidthPrice:   recvSettings.UploadBandwidthPrice,
		RevisionNumber:         recvSettings.RevisionNumber,
		Version:                recvSettings.Version,
	}
	return host, nil
}

// verifyRecentRevision confirms that the host and contractor agree upon the current
// state of the contract being revised.
func verifyRecentRevision(conn net.Conn, contract *SafeContract, hostVersion string) error {
	// send contract ID
	if err := encoding.WriteObject(conn, contract.header.ID()); err != nil {
		return errors.New("couldn't send contract ID: " + err.Error())
	}
	// read challenge
	var challenge crypto.Hash
	if err := encoding.ReadObject(conn, &challenge, 32); err != nil {
		return errors.New("couldn't read challenge: " + err.Error())
	}
	if build.VersionCmp(hostVersion, "1.3.0") >= 0 {
		crypto.SecureWipe(challenge[:16])
	}
	// sign and return
	sig := crypto.SignHash(challenge, contract.header.SecretKey)
	if err := encoding.WriteObject(conn, sig); err != nil {
		return errors.New("couldn't send challenge response: " + err.Error())
	}
	// read acceptance
	if err := modules.ReadNegotiationAcceptance(conn); err != nil {
		return errors.New("host did not accept revision request: " + err.Error())
	}
	// read last revision and signatures
	var lastRevision types.FileContractRevision
	var hostSignatures []types.TransactionSignature
	if err := encoding.ReadObject(conn, &lastRevision, 2048); err != nil {
		return errors.New("couldn't read last revision: " + err.Error())
	}
	if err := encoding.ReadObject(conn, &hostSignatures, 2048); err != nil {
		return errors.New("couldn't read host signatures: " + err.Error())
	}
	// Check that the unlock hashes match; if they do not, something is
	// seriously wrong. Otherwise, check that the revision numbers match.
	ourRev := contract.header.LastRevision()
	if lastRevision.UnlockConditions.UnlockHash() != ourRev.UnlockConditions.UnlockHash() {
		return errors.New("unlock conditions do not match")
	} else if lastRevision.NewRevisionNumber != ourRev.NewRevisionNumber {
		// If the revision number doesn't match try to commit potential
		// unapplied transactions and check again.
		if err := contract.managedCommitTxns(); err != nil {
			return errors.AddContext(err, "failed to commit transactions")
		}
		ourRev = contract.header.LastRevision()
		if lastRevision.NewRevisionNumber != ourRev.NewRevisionNumber {
			return &revisionNumberMismatchError{ourRev.NewRevisionNumber, lastRevision.NewRevisionNumber}
		}
	}
	// NOTE: we can fake the blockheight here because it doesn't affect
	// verification; it just needs to be above the fork height and below the
	// contract expiration (which was checked earlier).
	return modules.VerifyFileContractRevisionTransactionSignatures(lastRevision, hostSignatures, contract.header.EndHeight()-1)
}

// negotiateRevision sends a revision and actions to the host for approval,
// completing one iteration of the revision loop.
func negotiateRevision(conn net.Conn, rev types.FileContractRevision, secretKey crypto.SecretKey, height types.BlockHeight) (types.Transaction, error) {
	// create transaction containing the revision
	signedTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{rev},
		TransactionSignatures: []types.TransactionSignature{{
			ParentID:       crypto.Hash(rev.ParentID),
			CoveredFields:  types.CoveredFields{FileContractRevisions: []uint64{0}},
			PublicKeyIndex: 0, // renter key is always first -- see formContract
		}},
	}
	// sign the transaction
	encodedSig := crypto.SignHash(signedTxn.SigHash(0, height), secretKey)
	signedTxn.TransactionSignatures[0].Signature = encodedSig[:]

	// send the revision
	if err := encoding.WriteObject(conn, rev); err != nil {
		return types.Transaction{}, errors.New("couldn't send revision: " + err.Error())
	}
	// read acceptance
	if err := modules.ReadNegotiationAcceptance(conn); err != nil {
		return types.Transaction{}, errors.New("host did not accept revision: " + err.Error())
	}

	// send the new transaction signature
	if err := encoding.WriteObject(conn, signedTxn.TransactionSignatures[0]); err != nil {
		return types.Transaction{}, errors.New("couldn't send transaction signature: " + err.Error())
	}
	// read the host's acceptance and transaction signature
	// NOTE: if the host sends ErrStopResponse, we should continue processing
	// the revision, but return the error anyway.
	responseErr := modules.ReadNegotiationAcceptance(conn)
	if responseErr != nil && !errors.Contains(responseErr, modules.ErrStopResponse) {
		return types.Transaction{}, errors.New("host did not accept transaction signature: " + responseErr.Error())
	}
	var hostSig types.TransactionSignature
	if err := encoding.ReadObject(conn, &hostSig, 16e3); err != nil {
		return types.Transaction{}, errors.New("couldn't read host's signature: " + err.Error())
	}

	// add the signature to the transaction and verify it
	// NOTE: we can fake the blockheight here because it doesn't affect
	// verification; it just needs to be above the fork height and below the
	// contract expiration (which was checked earlier).
	verificationHeight := rev.NewWindowStart - 1
	signedTxn.TransactionSignatures = append(signedTxn.TransactionSignatures, hostSig)
	if err := signedTxn.StandaloneValid(verificationHeight); err != nil {
		return types.Transaction{}, err
	}

	// if the host sent ErrStopResponse, return it
	return signedTxn, responseErr
}

// newDownloadRevision revises the current revision to cover the cost of
// downloading data.
func newDownloadRevision(current types.FileContractRevision, downloadCost types.Currency) (types.FileContractRevision, error) {
	return current.PaymentRevision(downloadCost)
}

// newUploadRevision revises the current revision to cover the cost of
// uploading a sector.
func newUploadRevision(current types.FileContractRevision, merkleRoot crypto.Hash, price, collateral types.Currency) (types.FileContractRevision, error) {
	rev, err := current.PaymentRevision(price)
	if err != nil {
		return types.FileContractRevision{}, err
	}

	// Check that there is enough collateral to cover the cost.
	if rev.MissedHostOutput().Value.Cmp(collateral) < 0 {
		return types.FileContractRevision{}, types.ErrRevisionCollateralTooLow
	}

	// move collateral from host to void
	rev.SetMissedHostPayout(rev.MissedHostOutput().Value.Sub(collateral))
	voidOutput, err := rev.MissedVoidOutput()
	if err != nil {
		return types.FileContractRevision{}, errors.AddContext(err, "failed to get void output")
	}
	err = rev.SetMissedVoidPayout(voidOutput.Value.Add(collateral))
	if err != nil {
		return types.FileContractRevision{}, errors.AddContext(err, "failed to set void output")
	}

	// set new filesize and Merkle root
	rev.NewFileSize += modules.SectorSize
	rev.NewFileMerkleRoot = merkleRoot
	return rev, nil
}

// performSessionHandshake conducts the initial handshake exchange of the
// renter-host protocol. During the handshake, a shared secret is established,
// which is used to initialize an AEAD cipher. This cipher must be used to
// encrypt subsequent RPCs.
func performSessionHandshake(conn net.Conn, hostPublicKey types.UploPublicKey) (cipher.AEAD, modules.LoopChallengeRequest, error) {
	// generate a session key
	xsk, xpk := crypto.GenerateX25519KeyPair()

	// send our half of the key exchange
	req := modules.LoopKeyExchangeRequest{
		PublicKey: xpk,
		Ciphers:   []types.Specifier{modules.CipherChaCha20Poly1305},
	}
	extendDeadline(conn, modules.NegotiateSettingsTime)
	if err := encoding.NewEncoder(conn).EncodeAll(modules.RPCLoopEnter, req); err != nil {
		return nil, modules.LoopChallengeRequest{}, err
	}
	// read host's half of the key exchange
	var resp modules.LoopKeyExchangeResponse
	if err := encoding.NewDecoder(conn, encoding.DefaultAllocLimit).Decode(&resp); err != nil {
		return nil, modules.LoopChallengeRequest{}, err
	}
	// validate the signature before doing anything else; don't want to punish
	// the "host" if we're talking to an imposter
	var hpk crypto.PublicKey
	copy(hpk[:], hostPublicKey.Key)
	var sig crypto.Signature
	copy(sig[:], resp.Signature)
	if err := crypto.VerifyHash(crypto.HashAll(req.PublicKey, resp.PublicKey), hpk, sig); err != nil {
		return nil, modules.LoopChallengeRequest{}, err
	}
	// check for compatible cipher
	if resp.Cipher != modules.CipherChaCha20Poly1305 {
		return nil, modules.LoopChallengeRequest{}, errors.New("host selected unsupported cipher")
	}
	// derive shared secret, which we'll use as an encryption key
	cipherKey := crypto.DeriveSharedSecret(xsk, resp.PublicKey)

	// use cipherKey to initialize an AEAD cipher
	aead, err := chacha20poly1305.New(cipherKey[:])
	if err != nil {
		build.Critical("could not create cipher")
		return nil, modules.LoopChallengeRequest{}, err
	}

	// read host's challenge
	var challengeReq modules.LoopChallengeRequest
	if err := modules.ReadRPCMessage(conn, aead, &challengeReq, modules.RPCMinLen); err != nil {
		return nil, modules.LoopChallengeRequest{}, err
	}
	return aead, challengeReq, nil
}
