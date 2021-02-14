# Modules

The modules package is the top-level package for all modules. It contains the interface for each module, sub-packages which implement said modules and other shared constants and code which needs to be accessible within all sub-packages.

## Top-Level Modules
- [Consensus](#consensus)
- [Explorer](#explorer)
- [Gateway](#gateway)
- [Host](#host)
- [Miner](#miner)
- [Renter](#renter)
- [Transaction Pool](#transaction-pool)
- [Wallet](#wallet)

## Subsystems
- [Alert System](#alert-system)
- [Append-Only Persist](#append-only-persist)
- [Dependencies](#dependencies)
- [Negotiate](#negotiate)
- [Network Addresses](#network-addresses)
- [uplod Configuration](#uplod-configuration)
- [Skyfile Reader](#skyfile-reader)
- [Skylink](#skylink)
- [Skynet](#skynet)
- [Skynetbackup](#skynet-backup)
- [UploPath](#uplopath)
- [Storage Manager](#storage-manager)

### Consensus
**Key Files**
- [consensus.go](./consensus.go)
- [README.md](./consensus/README.md)

*TODO* 
  - fill out module explanation

### Explorer
**Key Files**
- [explorer.go](./explorer.go)
- [README.md](./explorer/README.md)

*TODO* 
  - fill out module explanation

### Gateway
**Key Files**
- [gateway.go](./gateway.go)
- [README.md](./gateway/README.md)

*TODO* 
  - fill out module explanation

### Host
**Key Files**
- [host.go](./host.go)
- [README.md](./host/README.md)

*TODO* 
  - fill out module explanation

### Miner
**Key Files**
- [miner.go](./miner.go)
- [README.md](./miner/README.md)

*TODO* 
  - fill out module explanation

### Renter
**Key Files**
- [renter.go](./renter.go)
- [README.md](./renter/README.md)

*TODO* 
  - fill out module explanation

### Transaction Pool
**Key Files**
- [transactionpool.go](./transactionpool.go)
- [README.md](./transactionpool/README.md)

*TODO* 
  - fill out module explanation

### Wallet
**Key Files**
- [wallet.go](./wallet.go)
- [README.md](./wallet/README.md)

*TODO* 
  - fill out subsystem explanation

### Alert System
**Key Files**
- [alert.go](./alert.go)

The Alert System provides the `Alerter` interface and an implementation of the interface which can be used by modules which need to be able to register alerts in case of irregularities during runtime. An `Alert` provides the following information:

- **Message**: Some information about the issue
- **Cause**: The cause for the issue if it is known
- **Module**: The name of the module that registered the alert
- **Severity**: The severity level associated with the alert

The following levels of severity are currently available:

- **Unknown**: This should never be used and is a safeguard against developer errors
- **Warning**: Warns the user about potential issues which might require preventive actions
- **Error**: Alerts the user of an issue that requires immediate action to prevent further issues like loss of data
- **Critical**: Indicates that a critical error is imminent. e.g. lack of funds causing contracts to get lost

### Dependencies
**Key Files**
- [dependencies.go](./dependencies.go)

*TODO* 
  - fill out subsystem explanation

### Negotiate
**Key Files**
- [negotiate.go](./negotiate.go)

*TODO* 
  - fill out subsystem explanation

### Network Addresses
**Key Files**
- [netaddress.go](./netaddress.go)

*TODO* 
  - fill out subsystem explanation

### Packing
**Key Files**
- [packing.go](./packing.go)

The smallest amount of data that can be uploaded to the Uplo network is 4 MiB. This limitation can be overcome by packing multiple files together. The upload batching commands can pack a bunch of small files into the same sector, producing a unique skylink for each file.

Batch uploads work much the same as uploads, except that a JSON manifest is provided which pairs a list of source files to their destination uplopaths. Every file in the manifest must be smaller than 4 MiB. The packing algorithm attempts to optimally pack the list of files into as few chunks as possible, where each chunk is 4 MiB in size.

### uplod Configuration
**Key Files**
- [uplodconfig.go](./uplodconfig.go)

*TODO* 
  - fill out subsystem explanation

### Skyfile Reader

**Key Files**
-[skyfilereader.go](./skyfilereader.go)

**TODO**
  - fill out subsystem explanation

### Skylink

**Key Files**
-[skylink.go](./skylink.go)

The skylink is a format for linking to data sectors stored on the Uplo network.
In addition to pointing to a data sector, the skylink contains a lossy offset an
length that point to a data segment within the sector, allowing multiple small
files to be packed into a single sector.

All told, there are 32 bytes in a skylink for encoding the Merkle root of the
sector being linked, and 2 bytes encoding a link version, the offset, and the
length of the sector being fetched.

For more information, check out the documentation in the [skylink.go](./skylink.go) file.

### Skynet

**Key Files**
-[skynet.go](./skynet.go)

**TODO**
  - fill out subsystem explanation

### Skynet Backup

**Key Files**
-[skynetbackup.go](./skynetbackup.go)

The Skynet Backup subsystem handles persistence for creating and reading skynet
backup data. These backups contain all the information needed to restore
a Skyfile with the original Skylink.

### UploPath
**Key Files**
- [uplopath.go](./uplopath.go)

Uplopaths are the format of filesystem paths on the Uplo network. Internally they
are handled as linux paths and use the `/` separator. Uplopaths are used to
identify directories on the Uplo network as well as files.  When manipulating
Uplopaths in memory the `strings` package should be used so that the `/`
separator can be enforced. When Uplopaths are being translated to System paths,
the `filepath` package is used to ensure the correct path separator is used for
the OS that is running.

### Storage Manager
**Key Files**
- [storagemanager.go](./storagemanager.go)

*TODO* 
  - fill out subsystem explanation
