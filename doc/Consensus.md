Consensus Rules
===============

This document is meant to provide a good high level overview of the Uplo
cryptosystem, but does not fully explain all of the small details. The most
accurate explanation of the consensus rules is the consensus package (and all
dependencies).

This document will be more understandable if you have a general understanding
of proof of work blockchains, and does not try to build up from first
principles.

Cryptographic Algorithms
------------------------

Uplo uses cryptographic hashing and cryptographic signing, each of which has
many potentially secure algorithms that can be used. We acknowledge our
inexperience, and that we have chosen these algorithms not because of our own
confidence in their properties, but because other people seem confident in
their properties.

For hashing, our primary goal is to use an algorithm that cannot be merge mined
with Bitcoin, even partially. A secondary goal is hashing speed on consumer
hardware, including phones and other low power devices.

For signing, our primary goal is verification speed. A secondary goal is an
algorithm that supports HD keys. A tertiary goal is an algorithm that supports
threshold signatures.

#### Hashing: blake2b

  [blake2b](http://en.wikipedia.org/wiki/BLAKE_%28hash_function%29#BLAKE2 "Wiki page") has been chosen as a hashing algorithm because it is fast, it has had
  substantial review, and it has invulnerability to length extension attacks.
  Another particularly important feature of BLAKE2b is that it is not SHA-2. We
  wish to avoid merge mining with Bitcoin, because that may result in many
  apathetic Bitcoin miners mining on our blockchain, which may make soft forks
  harder to coordinate.

#### Signatures: variable type signatures

  Each public key will have an specifier (a 16 byte array) and a byte slice
  containing an encoding of the public key. The specifier will tell the
  signature verification which signing algorithm to use when verifying a
  signature. Each signature will be a byte slice, the encoding can be
  determined by looking at the specifier of the corresponding public key.

  This method allows new signature types to be easily added to the currency in
  a way that does not invalidate existing outputs and keys. Adding a new
  signature type requires a soft fork, but allows easy protection against
  cryptographic breaks, and easy migration to new cryptography if there are any
  breakthroughs in areas like verification speed, ring signatures, etc.

  Allowed algorithms:

  ed25519: The specifier must match the string "ed25519". The public key
  must be encoded into 32 bytes. Signatures and public keys will need to
  follow the ed25519 specification. More information can be found at
  ed25519.cr.yp.to

  entropy: The specifier must match the string "entropy". The signature will
  always be invalid. This provides a way to add entropy buffers to
  SpendCondition objects to protect low entropy information, while being able
  to prove that the entropy buffers are invalid public keys.

  There are plans to also add ECDSA secp256k1 and Schnorr secp256k1. New
  signing algorithms can be added to Uplo through a soft fork, because
  unrecognized algorithm types are always considered to have valid signatures.

Currency
--------

The Uplo cryptosystem has two types of currency. The first is the Uplocoin.
Uplocoins are generated every block and distributed to the miners. These miners
can then use the Uplocoins to fund file contracts, or can send the Uplocoins to
other parties. The Uplocoin is represented by an infinite precision unsigned
integer.

The second currency in the Uplo cryptosystem is the Uplofund, which is a special
asset limited to 10,000 indivisible units. Each time a file contract payout is
made, 3.9% of the payout is put into the uplofund pool. The number of Uplocoins
in the uplofund pool must always be divisible by 10,000; the number of coins
taken from the payout is rounded down to the nearest 10,000. The uplofund is
also represented by an infinite precision unsigned integer.

Uplofund owners can collect the Uplocoins in the uplofund pool. For every 10,000
Uplocoins added to the uplofund pool, a uplofund owner can withdraw 1 Uplocoin.
Approx. 8790 uplofunds are owned by Nebulous Inc. The remaining uplofunds are
owned by early backers of the Uplo project.

There are future plans to enable sidechain compatibility with Uplo. This would
allow other currencies such as Bitcoin to be spent in all the same places that
the Uplocoin can be spent.

Marshalling
-----------

Many of the Uplo types need to be hashed at some point, which requires having a
consistent algorithm for marshalling types into a set of bytes that can be
hashed. The following rules are used for hashing:

 - Integers are little-endian, and are always encoded as 8 bytes.
 - Bools are encoded as one byte, where zero is false and one is true.
 - Variable length types such as strings are prefaced by 8 bytes containing
   their length.
 - Arrays and structs are encoded as their individual elements concatenated
   together. The ordering of the struct is determined by the struct definition.
   There is only one way to encode each struct.
 - The Currency type (an infinite precision integer) is encoded in big endian
   using as many bytes as necessary to represent the underlying number. As it
   is a variable length type, it is prefixed by 8 bytes containing the length.

Block Size
----------

The maximum block size is 2e6 bytes. There is no limit on transaction size,
though it must fit inside of the block. Most miners enforce a size limit of
16e3 bytes per transaction.

Block Timestamps
----------------

Each block has a minimum allowed timestamp. The minimum timestamp is found by
taking the median timestamp of the previous 11 blocks. If there are not 11
previous blocks, the genesis timestamp is used repeatedly.

Blocks will be rejected if they are timestamped more than three hours in the
future, but can be accepted again once enough time has passed.

Block ID
--------

The ID of a block is derived using:
	Hash(Parent Block ID + 64 bit Nonce + Timestamp + Block Merkle Root)

The block Merkle root is obtained by creating a Merkle tree whose leaves are
the hashes of the miner outputs (one leaf per miner output), and the hashes of 
the transactions (one leaf per transaction).

Block Target
------------

A running tally is maintained which keeps the total difficulty and total time
passed across all blocks. The total difficulty can be divided by the total time
to get a hashrate. The total is multiplied by 0.995 each block, to keep
exponential preference on recent blocks with a half life of 144 data points.
This is about 24 hours. This estimated hashrate is assumed to closely match the
actual hashrate on the network.

There is a target block time. If the difficulty increases or decreases, the
total amount of time that has passed will be more or less than the target amount
of time passed for the current height. To counteract this, the target block time
for each block is adjusted based on how far away from the desired total time
passed the current total time passed is. If the total time passed is too low,
blocks are targeted to be slightly longer, which helps to correct the network.
And if the total time passed is too high, blocks are targeted to be slightly
shorter, to help correct the network.

High variance in block times means that the corrective action should not be very
strong if the total time passed has only missed the target time passed by a few
hours. But if the total time passed is significantly off, the block time
corrections should be much stronger. The square of the total deviation is used
to figure out what the adjustment should be. At 10,000 seconds variance (about 3
hours), blocks will be adjusted by 10 seconds each. At 20,000 seconds, blocks
will be adjusted by 40 seconds each, a 4x adjustment for 2x the error. And at
40,000 seconds, blocks will be adjusted by 160 seconds each, and so on.

The total amount of blocktime adjustment is capped to 1/3 and 3x the target
blocktime, to prevent too much disruption on the network. If blocks are actually
coming out 3x as fast as intended, there will be a (temporary) significant
increase on the amount of strain on nodes to process blocks. And at 1/3 the
target blocktime, the total blockchain throughput will decrease dramatically.

Finally, one extra cap is applied to the difficulty adjustment - the difficulty
of finding a block is not allowed to change more than 0.4% every block. This
maps to a total possible difficulty change of 55x across 1008 blocks. This clamp
helps to prevent wild swings when the hashrate increases or decreases rapidly on
the network, and it also limits the amount of damange that a malicious attacker
can do if performing a difficulty raising attack.

It should be noted that the timestamp of a block does not impact the difficulty
of the block that follows it, as this can be exploited by selfish miners.
Instead, a block does not impact the difficulty adjustment for 2 blocks. This
can still be exploited by selfish miners, however the gains they get from
exploiting this are massively reduced and make no substantial impact on the
overall effectiveness of selfish mining.

Block Subsidy
-------------

The coinbase for a block is (300,000 - height) * 10^24, with a minimum of
30,000 \* 10^24. Any miner fees get added to the coinbase to create the block
subsidy. The block subsidy is then given to multiple outputs, called the miner
payouts. The total value of the miner payouts must equal the block subsidy.

The ids of the outputs created by the miner payouts is determined by taking the
block id and concatenating the index of the payout that the output corresponds
to.

The outputs created by the block subsidy cannot be spent for 50 blocks, and are
not considered a part of the consensus set until 50 blocks have transpired.
This limitation is in place because a simple blockchain reorganization is
enough to invalidate the output; double spend attacks and false spend attacks
are much easier to execute.

Transactions
------------

A Transaction is composed of the following:

- Uplocoin Inputs
- Uplocoin Outputs
- File Contracts
- File Contract Revisions
- Storage Proofs
- Uplofund Inputs
- Uplofund Outputs
- Miner Fees
- Arbitrary Data
- Transaction Signatures

The sum of all the Uplocoin inputs must equal the sum of all the miner fees,
Uplocoin outputs, and file contract payouts. There can be no leftovers. The sum
of all uplofund inputs must equal the sum of all uplofund outputs.

Several objects have unlock hashes. An unlock hash is the Merkle root of the
'unlock conditions' object. The unlock conditions contain a timelock, a number
of required signatures, and a set of public keys that can be used during
signing.

The Merkle root of the unlock condition objects is formed by taking the Merkle
root of a tree whose leaves are the timelock, the public keys (one leaf per
key), and the number of signatures. This ordering is chosen specifically
because the timelock and the number of signatures are low entropy. By using
random data as the first and last public key, you can make it safe to reveal
any of the public keys without revealing the low entropy items.

The unlock conditions cannot be satisfied until enough signatures have
provided, and until the height of the blockchain is at least equal to the value
of the timelock.

The unlock conditions contains a set of public keys which can each be used only
once when providing signatures. The same public key can be listed twice, which
means that it can be used twice. The number of required signatures indicates
how many public keys must be used to validate the input. If required signatures
is '0', the input is effectively 'anyone can spend'. If the required signature
count is greater than the number of public keys, the input is unspendable.
There must be exactly enough signatures. For example, if there are 3 public
keys and only two required signatures, then only two signatures can be included
into the transaction.

Uplocoin Inputs
--------------

Each input spends an output. The output being spent must exist in the consensus
set. The 'value' field of the output indicates how many Uplocoins must be used
in the outputs of the transaction. Valid outputs are miner fees, Uplocoin
outputs, and contract payouts.

Uplocoin Outputs
---------------

Uplocoin outputs contain a value and an unlock hash (also called a coin
address). The unlock hash is the Merkle root of the spend conditions that must
be met to spend the output.

File Contracts
--------------

A file contract is an agreement by some party to prove they have a file at a
given point in time. The contract contains the Merkle root of the data being
stored, and the size in bytes of the data being stored.

The Merkle root is formed by breaking the file into 64 byte segments and
hashing each segment to form the leaves of the Merkle tree. The final segment
is not padded out.

The storage proof must be submitted between the 'WindowStart' and 'WindowEnd'
fields of the contract. There is a 'Payout', which indicates how many Uplocoins
are given out when the storage proof is provided. 3.9% of this payout (rounded
down to the nearest 10,000) is put aside for the owners of uplofunds. If the
storage proof is provided and is valid, the remaining payout is put in an
output spendable by the 'valid proof spend hash', and if a valid storage proof
is not provided to the blockchain by 'end', the remaining payout is put in an
output spendable by the 'missed proof spend hash'.

All contracts must have a non-zero payout, 'start' must be before 'end', and
'start' must be greater than the current height of the blockchain. A storage
proof is acceptable if it is submitted in the block of height 'end'.

File contracts are created with a 'Revision Hash', which is the Merkle root of
an unlock conditions object. A 'file contract revision' can be submitted which
fulfills the unlock conditions object, resulting in the file contract being
replaced by a new file contract, as specified in the revision.

File Contract Revisions
-----------------------

A file contract revision modifies a contract. File contracts have a revision
number, and any revision submitted to the blockchain must have a higher
revision number in order to be valid. Any field can be changed except for the
payout - Uplocoins cannot be added to or removed from the file contract during a
revision, though the destination upon a successful or unsuccessful storage
proof can be changed.

The greatest application for file contract revisions is file-diff channels - a
file contract can be edited many times off-blockchain as a user uploads new or
different content to the host. This improves the overall scalability of Uplo.

Storage Proofs
--------------

A storage proof transaction is any transaction containing a storage proof.
Storage proof transactions are not allowed to have Uplocoin or uplofund outputs,
and are not allowed to have file contracts.

When creating a storage proof, you only prove that you have a single 64 byte
segment of the file. The piece that you must prove you have is chosen
randomly using the contract id and the id of the 'trigger block'.  The
trigger block is the block at height 'Start' - 1, where 'Start' is the value
'Start' in the contract that the storage proof is fulfilling.

The file is composed of 64 byte segments whose hashes compose the leaves of a
Merkle tree. When proving you have the file, you must prove you have one of the
leaves. To determine which leaf, take the hash of the contract id concatenated
to the trigger block id, then take the numerical value of the result modulus
the number of segments:

	Hash(file contract id + trigger block id) % num segments

The proof is formed by providing the 64 byte segment, and then the missing
hashes required to fill out the remaining tree. The total size of the proof
will be 64 bytes + 32 bytes * log(num segments), and can be verified by anybody
who knows the root hash and the file size.

Storage proof transactions are not allowed to have Uplocoin outputs, uplofund
outputs, or contracts. All outputs created by the storage proofs cannot be
spent for 50 blocks.

These limits are in place because a simple blockchain reorganization can change
the trigger block, which will invalidate the storage proof and therefore the
entire transaction. This makes double spend attacks and false spend attacks
significantly easier to execute.

Uplofund Inputs
--------------

A uplofund input works similar to a Uplocoin input. It contains the id of a
uplofund output being spent, and the unlock conditions required to spend the
output.

A special output is created when a uplofund output is used as input. All of the
Uplocoins that have accrued in the uplofund since its last spend are sent to the
'claim spend hash' found in the uplofund output, which is a normal Uplocoin
address. The value of the Uplocoin output is determined by taking the size of
the Uplocoin pool when the output was created and comparing it to the current
size of the Uplocoin pool. The equation is:

	((Current Pool Size - Previous Pool Size) / 10,000) * uplofund quantity

Like the miner outputs and the storage proof outputs, the uplofund output cannot
be spent for 50 blocks because the value of the output can change if the
blockchain reorganizes. Reorganizations will not however cause the transaction
to be invalidated, so the ban on contracts and outputs does not need to be in
place.

Uplofund Outputs
---------------

Like Uplocoin outputs, uplofund outputs contain a value and an unlock hash. The
value indicates the number of uplofunds that are put into the output, and the
unlock hash is the Merkle root of the unlock conditions object which allows the
output to be spent.

Uplofund outputs also contain a claim unlock hash field, which indicates the
unlock hash of the Uplocoin output that is created when the uplofund output is
spent. The value of the output that gets created will depend on the growth of
the Uplocoin pool between the creation and the spending of the output. This
growth is measured by storing a 'claim start', which indicates the size of the
uplofund pool at the moment the uplofund output was created.

Miner Fees
----------

A miner fee is a volume of Uplocoins that get added to the block subsidy.

Arbitrary Data
--------------

Arbitrary data is a set of data that is ignored by consensus. In the future, it
may be used for soft forks, paired with 'anyone can spend' transactions. In the
meantime, it is an easy way for third party applications to make use of the
Uplocoin blockchain.

Transaction Signatures
----------------------

Each signature points to a single public key index in a single unlock
conditions object. No two signatures can point to the same public key index for
the same set of unlock conditions.

Each signature also contains a timelock, and is not valid until the blockchain
has reached a height equal to the timelock height.

Signatures also have a 'covered fields' object, which indicates which parts of
the transaction get included in the signature. There is a 'whole transaction'
flag, which indicates that every part of the transaction except for the
signatures gets included, which eliminates any malleability outside of the
signatures. The signatures can also be individually included, to enforce that
your signature is only valid if certain other signatures are present.

If the 'whole transaction' is not set, all fields need to be added manually,
and additional parties can add new fields, meaning the transaction will be
malleable. This does however allow other parties to add additional inputs,
fees, etc. after you have signed the transaction without invalidating your
signature. If the whole transaction flag is set, all other elements in the
covered fields object must be empty except for the signatures field.

The covered fields object contains a slice of indexes for each element of the
transaction (Uplocoin inputs, miner fees, etc.). The slice must be sorted, and
there can be no repeated elements.

Entirely nonmalleable transactions can be achieved by setting the 'whole
transaction' flag and then providing the last signature, including every other
signature in your signature. Because no frivolous signatures are allowed, the
transaction cannot be changed without your signature being invalidated.
