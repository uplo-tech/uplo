# Build
The build package contains high level helper functions.

## Subsystems
 - [appdata](#appdata)
 - [commit](#commit)
 - [critical](#critical)
 - [debug](#debug)
 - [errors](#errors)
 - [release](#release)
 - [testing](#testing)
 - [url](#url)
 - [var](#var)
 - [version](#version)
 - [vlong](#vlong)

## Appdata
### Key Files
 - [appdata.go](./appdata.go)
 - [appdata_test.go](./appdata_test.go)

The Appdata subsystem is responsible for providing information about various Uplo
application data. This subsystem is used to interact with any environment
variables that are set by the user.

**Environment Variables**
 - `UPLO_API_PASSWORD` is the uploAPIPassword environment variable that sets a
   custom API password
 - `UPLO_DATA_DIR` uplodataDir is the environment variable that tells uplod where 
    to put the general uplo data, e.g. api password, configuration, logs, etc.
 - `uplod_DATA_DIR` uplodDataDir is the environment variable which tells uplod 
    where to put the uplod-specific data
 - `UPLO_WALLET_PASSWORD` is the uploWalletPassword environment variable that can
   enable auto unlocking the wallet

## Build Flags
### Key Files
 - [debug_off.go](./debug_off.go)
 - [debug_on.go](./debug_on.go)
 - [release_dev.go](./release_dev.go)
 - [release_standard.go](./release_standard.go)
 - [release_testing.go](./release_testing.go)
 - [vlong_off.go](./vlong_off.go)
 - [vlong_on.go](./vlong_on.go)

TODO...

## Commit
TODO...

## Critical
TODO...

## Errors
TODO...

## Testing
TODO...

## URL
### Key Files
 - [url.go](./url.go)

The URL subsystem is responsible for providing information about Uplo URLs that
are in use.

## Var
TODO...

## Version
TODO...
