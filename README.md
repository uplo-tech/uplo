# [![Uplo Logo](https://static.uplo.tech/assets/images/svg/git_logo.svg)](http://uplo.tech)

[![Build Status](https://github.com/uplo-tech/uplo/badges/master/pipeline.svg)](https://github.com/uplo-tech/uplo/commits/master)
[![Coverage Report](https://github.com/uplo-tech/uplo/badges/master/coverage.svg)](https://github.com/uplo-tech/uplo/commits/master)
[![GoDoc](https://godoc.org/github.com/uplo-tech/uplo?status.svg)](https://godoc.org/github.com/uplo-tech/uplo)
[![Go Report Card](https://goreportcard.com/badge/github.com/uplo-tech/uplo)](https://goreportcard.com/report/github.com/uplo-tech/uplo)
[![License MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://img.shields.io/badge/License-MIT-brightgreen.svg)

Uplo is a decentralized cloud storage platform that radically alters the
landscape of cloud storage. By leveraging smart contracts, client-side
encryption, and sophisticated redundancy (via Reed-Solomon codes), Uplo allows
users to safely store their data with hosts that they do not know or trust.
The result is a cloud storage marketplace where hosts compete to offer the
best service at the lowest price. And since there is no barrier to entry for
hosts, anyone with spare storage capacity can join the network and start
making money.

![UI](https://static.uplo.tech/assets/images/png/screenshot_wallet.png)

Traditional cloud storage has a number of shortcomings. Users are limited to a
few big-name offerings: Google, Microsoft, Amazon. These companies have little
incentive to encrypt your data or make it easy to switch services later. Their
code is closed-source, and they can lock you out of your account at any time.

We believe that users should own their data. Uplo achieves this by replacing
the traditional monolithic cloud storage provider with a blockchain and a
swarm of hosts, each of which stores an encrypted fragment of your data. Since
the fragments are redundant, no single host can hold your data hostage: if
they jack up their price or go offline, you can simply download from a
different host. In other words, trust is removed from the equation, and
switching to a different host is painless. Stripped of these unfair
advantages, hosts must compete solely on the quality and price of the storage
they provide.

Uplo can serve as a replacement for personal backups, bulk archiving, content
distribution, and more. For developers, Uplo is a low-cost alternative to
Amazon S3. Storage on Uplo is a full order of magnitude cheaper than on S3,
with comparable bandwidth, latency, and durability. Uplo works best for static
content, especially media like videos, music, and photos.

Distributing data across many hosts automatically confers several advantages.
The most obvious is that, just like BitTorrent, uploads and downloads are
highly parallel. Given enough hosts, Uplo can saturate your bandwidth. Another
advantage is that your data is spread across a wide geographic area, reducing
latency and safeguarding your data against a range of attacks.

It is important to note that users have full control over which hosts they
use. You can tailor your host set for minimum latency, lowest price, widest
geographic coverage, or even a strict whitelist of IP addresses or public
keys.

At the core of Uplo is a blockchain that closely resembles Bitcoin. Transactions
are conducted in Uplocoin, a cryptocurrency. The blockchain is what allows Uplo to
enforce its smart contracts without relying on centralized authority. To acquire
Uplocoins, use an exchange such as [Binance](https://binance.com),
[Bittrex](https://bittrex.com), [Shapeshift](https://shapeshift.io), or
[Poloniex](https://poloniex.com).

To get started with Uplo, check out the guides below:

- [Storing files with Uplo-UI](https://blog.uplo.tech/a-guide-to-uplo-ui-v1-4-0-7ec3dfcae35a)
- [How to Store Data on Uplo](https://blog.uplo.tech/getting-started-with-private-decentralized-cloud-storage-c9565dc8c854)
- [How to Become a Uplo Host](https://blog.uplo.tech/how-to-run-a-host-on-uplo-2159ebc4725)
- [Using the Uplo API](https://blog.uplo.tech/api-quickstart-guide-f1d160c05235)


Usage
-----

Uplo is ready for use with small sums of money and non-critical files, but
until the network has a more proven track record, we advise against using it
as a sole means of storing important data.

This release comes with 2 binaries, uplod and uploc. uplod is a background service,
or "daemon," that runs the Uplo protocol and exposes an HTTP API on port 8480.
uploc is a command-line client that can be used to interact with uplod in a
user-friendly way. There is also a graphical client,
[Uplo-UI](https://github.com/uplo-tech/uplo-UI), which is the preferred way of
using Uplo for most users. For interested developers, the uplod API is documented
at [uplo.tech/docs](https://uplo.tech/docs/).

uplod and uploc are run via command prompt. On Windows, you can just double-
click uplod.exe if you don't need to specify any command-line arguments.
Otherwise, navigate to its containing folder and click File->Open command
prompt. Then, start the uplod service by entering `uplod` and pressing Enter.
The command prompt may appear to freeze; this means uplod is waiting for
requests. Windows users may see a warning from the Windows Firewall; be sure
to check both boxes ("Private networks" and "Public networks") and click
"Allow access." You can now run `uploc` (in a separate command prompt) or Uplo-
UI to interact with uplod. From here, you can send money, upload and download
files, and advertise yourself as a host.

Building From Source
--------------------

To build from source, [Go 1.13 or above must be
installed](https://golang.org/doc/install) on the system. Clone the repo and run
`make`:

```
git clone https://github.com/uplo-tech/uplo
cd uplo && make dependencies && make
```

This will install the `uplod` and `uploc` binaries in your `$GOPATH/bin` folder.
(By default, this is `$HOME/go/bin`.)

You can also run `make test` and `make test-long` to run the short and full test
suites, respectively. Finally, `make cover` will generate code coverage reports
for each package; they are stored in the `cover` folder and can be viewed in
your browser.

Official Releases
--------------------
Official binaries can be found under
[Releases](https://github.com/uplo-tech/uplo/-/releases) or on
[uplo.tech](https://uplo.tech/get-started).

Additionally, an official Docker image can be found
[here](https://hub.docker.com/r/uplo-coin/uplo).
