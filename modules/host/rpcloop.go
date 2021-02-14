package host

import (
	"crypto/cipher"
	"net"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// An rpcSession contains the state of an RPC session with a renter.
type rpcSession struct {
	conn      net.Conn
	aead      cipher.AEAD
	so        storageObligation
	challenge [16]byte
}

// extendDeadline extends the read/write deadline on the underlying connection
// by d.
func (s *rpcSession) extendDeadline(d time.Duration) {
	s.conn.SetDeadline(time.Now().Add(d))
}

// readRequest reads an encrypted RPC request from the renter.
func (s *rpcSession) readRequest(resp interface{}, maxLen uint64) error {
	return modules.ReadRPCRequest(s.conn, s.aead, resp, maxLen)
}

// readResponse reads an encrypted RPC response from the renter.
func (s *rpcSession) readResponse(resp interface{}, maxLen uint64) error {
	return modules.ReadRPCResponse(s.conn, s.aead, resp, maxLen)
}

// writeResponse sends an encrypted RPC response to the renter.
func (s *rpcSession) writeResponse(resp interface{}) error {
	return modules.WriteRPCResponse(s.conn, s.aead, resp, nil)
}

// writeError sends an encrypted RPC error to the renter.
func (s *rpcSession) writeError(err error) error {
	return modules.WriteRPCResponse(s.conn, s.aead, nil, err)
}

// managedRPCLoop reads new RPCs from the renter, each consisting of a single
// request and response. The loop terminates when the an RPC encounters an
// error or the renter sends modules.RPCLoopExit.
func (h *Host) managedRPCLoop(conn net.Conn) error {
	// read renter's half of key exchange
	conn.SetDeadline(time.Now().Add(rpcRequestInterval))
	var req modules.LoopKeyExchangeRequest
	if err := encoding.NewDecoder(conn, encoding.DefaultAllocLimit).Decode(&req); err != nil {
		return err
	}

	// check for a supported cipher
	var supportsChaCha bool
	for _, c := range req.Ciphers {
		if c == modules.CipherChaCha20Poly1305 {
			supportsChaCha = true
		}
	}
	if !supportsChaCha {
		encoding.NewEncoder(conn).Encode(modules.LoopKeyExchangeResponse{
			Cipher: modules.CipherNoOverlap,
		})
		return errors.New("no supported ciphers")
	}

	// generate a session key, sign it, and derive the shared secret
	xsk, xpk := crypto.GenerateX25519KeyPair()
	pubkeySig := crypto.SignHash(crypto.HashAll(req.PublicKey, xpk), h.secretKey)
	cipherKey := crypto.DeriveSharedSecret(xsk, req.PublicKey)

	// send our half of the key exchange
	resp := modules.LoopKeyExchangeResponse{
		Cipher:    modules.CipherChaCha20Poly1305,
		PublicKey: xpk,
		Signature: pubkeySig[:],
	}
	if err := encoding.NewEncoder(conn).Encode(resp); err != nil {
		return err
	}

	// use cipherKey to initialize an AEAD cipher
	aead, err := chacha20poly1305.New(cipherKey[:])
	if err != nil {
		build.Critical("could not create cipher")
		return err
	}
	// create the session object
	s := &rpcSession{
		conn: conn,
		aead: aead,
	}
	fastrand.Read(s.challenge[:])

	// send encrypted challenge
	challengeReq := modules.LoopChallengeRequest{
		Challenge: s.challenge,
	}
	if err := modules.WriteRPCMessage(conn, aead, challengeReq); err != nil {
		return err
	}

	// ensure we unlock any locked contracts when protocol ends
	defer func() {
		if len(s.so.OriginTransactionSet) != 0 {
			h.managedUnlockStorageObligation(s.so.id())
			s.so = storageObligation{}
		}
	}()

	// enter RPC loop
	rpcs := map[types.Specifier]func(*rpcSession) error{
		modules.RPCLoopLock:               h.managedRPCLoopLock,
		modules.RPCLoopUnlock:             h.managedRPCLoopUnlock,
		modules.RPCLoopSettings:           h.managedRPCLoopSettings,
		modules.RPCLoopFormContract:       h.managedRPCLoopFormContract,
		modules.RPCLoopRenewContract:      h.managedRPCLoopRenewContract,
		modules.RPCLoopRenewClearContract: h.managedRPCLoopRenewAndClearContract,
		modules.RPCLoopWrite:              h.managedRPCLoopWrite,
		modules.RPCLoopRead:               h.managedRPCLoopRead,
		modules.RPCLoopSectorRoots:        h.managedRPCLoopSectorRoots,
	}
	for {
		conn.SetDeadline(time.Now().Add(rpcRequestInterval))
		id, err := modules.ReadRPCID(conn, aead)
		if err != nil {
			h.log.Debugf("WARN: could not read RPC ID: %v", err)
			err = errors.Compose(err, s.writeError(err)) // try to write, even though this is probably due to a faulty connection
			return err
		} else if id == modules.RPCLoopExit {
			return nil
		}
		if rpcFn, ok := rpcs[id]; !ok {
			return errors.New("invalid or unknown RPC ID: " + id.String())
		} else if err := rpcFn(s); err != nil {
			return extendErr("incoming RPC"+id.String()+" failed: ", err)
		}
	}
}
