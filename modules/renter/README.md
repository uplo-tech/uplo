# Renter
The Renter is responsible for tracking and actively maintaining all of the files
that a user has uploaded to Uplo. This includes the location and health of these
files. The Renter, via the HostDB and the Contractor, is also responsible for
picking hosts and maintaining the relationship with them.

The renter is unique for having two different logs. The first is a general
renter activity log, and the second is a repair log. The repair log is intended
to be a high-signal log that tells users what files are being repaired, and
whether the repair jobs have been successful. Where there are failures, the
repair log should try and document what those failures were. Every message of
the repair log should be interesting and useful to a power user, there should be
no logspam and no messages that would only make sense to uplod developers.

## Submodules
The Renter has several submodules that each perform a specific function for the
Renter. This README will provide brief overviews of the submodules, but for more
detailed descriptions of the inner workings of the submodules the respective
README files should be reviewed.
 - Contractor
 - Filesystem
 - HostDB
 - Proto
 - Skynet Blocklist
 - Skynet Portals

### Contractor
The Contractor manages the Renter's contracts and is responsible for all
contract actions such as new contract formation and contract renewals. The
Contractor determines which contracts are GoodForUpload and GoodForRenew and
marks them accordingly.

### Filesystem
The Filesystem is responsible for ensuring that all of its supported file
formats can be accessed in a threadsafe manner. It doesn't handle any
persistence directly but instead relies on the underlying format's package to
handle that itself.

### HostDB
The HostDB curates and manages a list of hosts that may be useful for the renter
in storing various types of data. The HostDB is responsible for scoring and
sorting the hosts so that when hosts are needed for contracts high quality hosts
are provided. 

### Proto
The proto module implements the renter's half of the renter-host protocol,
including contract formation and renewal RPCs, uploading and downloading,
verifying Merkle proofs, and synchronizing revision states. It is a low-level
module whose functionality is largely wrapped by the Contractor.

### Skynet Blocklist
The Skynet Blocklist module manages the list of skylinks that the Renter wants
blocked. It also manages persisting the blocklist in an ACID and performant
manner.

### Skynet Portals
The Skynet Portals module manages the list of known Skynet portals that the
Renter wants to keep track of. It also manages persisting the list in an ACID
and performant manner.

## Subsystems
The Renter has the following subsystems that help carry out its
responsibilities.
 - [Filesystem Controllers](#filesystem-controllers)
 - [Fuse Subsystem](#fuse-subsystem)
 - [Fuse Manager Subsystem](#fuse-manager-subsystem)
 - [Persistence Subsystem](#persistence-subsystem)
 - [Memory Subsystem](#memory-subsystem)
 - [Worker Subsystem](#worker-subsystem)
 - [Download Subsystem](#download-subsystem)
 - [Download Streaming Subsystem](#download-streaming-subsystem)
 - [Download By Root Subsystem](#download-by-root-subsystem)
 - [Skyfile Subsystem](#skyfile-subsystem)
 - [Stream Buffer Subsystem](#stream-buffer-subsystem)
 - [Upload Subsystem](#upload-subsystem)
 - [Upload Streaming Subsystem](#upload-streaming-subsystem)
 - [Health and Repair Subsystem](#health-and-repair-subsystem)
 - [Backup Subsystem](#backup-subsystem)
 - [Refresh Paths Subsystem](#refresh-paths-subsystem)

### Filesystem Controllers
**Key Files**
 - [dirs.go](./dirs.go)
 - [files.go](./files.go)

*TODO* 
  - fill out subsystem explanation

#### Outbound Complexities
 - `DeleteFile` calls `callThreadedBubbleMetadata` after the file is deleted
 - `RenameFile` calls `callThreadedBubbleMetadata` on the current and new
   directories when a file is renamed

### Fuse Subsystem
**Key Files**
 - [fuse.go](./fuse.go)

The fuse subsystem enables mounting the renter as a virtual filesystem. When
mounted, the kernel forwards I/O syscalls on files and folders to the userland
code in this subsystem. For example, the `read` syscall is implemented by
downloading data from Uplo hosts.

Fuse is implemented using the `hanwen/go-fuse/v2` series of packages, primarily
`fs` and `fuse`. The fuse package recognizes a single node interface for files
and folders, but the renter has two structs, one for files and another for
folders. Each the fuseDirnode and the fuseFilenode implement the same Node
interfaces.

The fuse implementation is remarkably sensitive to small details. UID mistakes,
slow load times, or missing/incorrect method implementations can often destroy
an external application's ability to interact with fuse. Currently we use
ranger, Nautilus, vlc/mpv, and uplostream when testing if fuse is still working
well. More programs may be added to this list as we discover more programs that
have unique requirements for working with the fuse package.

The uplotest/renter suite has two packages which are useful for testing fuse. The
first is [fuse\_test.go](../../uplotest/renter/fuse_test.go), and the second is
[fusemock\_test.go](../../uplotest/renter/fusemock_test.go). The first file
leverages a testgroup with a renter, a miner, and several hosts to mimic the Uplo
network, and then mounts a fuse folder which uses the full fuse implementation.
The second file contains a hand-rolled implementation of a fake filesystem which
implements the fuse interfaces. Both have a commented out sleep at the end of
the test which, when uncommented, allows a developer to explore the final
mounted fuse folder with any system application to see if things are working
correctly.

The mocked fuse is useful for debugging issues related to the fuse
implementation. When using the renter implementation, it can be difficult to
determine whether something is not working because there is a bug in the renter
code, or whether something is not working because the fuse libraries are being
used incorrectly. The mocked fuse is an easy way to replicate any desired
behavior and check for misunderstandings that the programmer may have about how
the fuse librires are meant to be used.

### Fuse Manager Subsystem
**Key Files**
 - [fusemanager.go](./fusemanager.go)

The fuse manager subsystem keeps track of multiple fuse directories that are
mounted at the same time. It maintains a list of mountpoints, and maps to the
fuse filesystem object that is mounted at those point. Only one folder can be
mounted at each mountpoint, but the same folder can be mounted at many
mountpoints.

When debugging fuse, it can be helpful to enable the 'Debug' option when
mounting a filesystem. This option is commented out in the fuse manager in
production, but searching for 'Debug:' in the file will reveal the line that can
be uncommented to enable debugging. Be warned that when debugging is enabled,
fuse becomes incredibly verbose.

Upon shutdown, the fuse manager will only attempt to unmount each folder one
time. If the folder is busy or otherwise in use by another application, the
unmount will fail and the user will have to manually unmount using `fusermount`
or `umount` before that folder becomes available again. To the best of our
current knowledge, there is no way to force an unmount.

### Persistence Subsystem
**Key Files**
 - [persist_compat.go](./persist_compat.go)
 - [persist.go](./persist.go)

*TODO* 
  - fill out subsystem explanation

### Memory Subsystem
**Key Files**
 - [memory.go](./memory.go)

The memory subsystem acts as a limiter on the total amount of memory that the
renter can use. The memory subsystem does not manage actual memory, it's really
just a counter. When some process in the renter wants to allocate memory, it
uses the 'Request' method of the memory manager. The memory manager will block
until enough memory has been returned to allow the request to be granted. The
process is then responsible for calling 'Return' on the memory manager when it
is done using the memory.

The memory manager is initialized with a base amount of memory. If a request is
made for more than the base memory, the memory manager will block until all
memory has been returned, at which point the memory manager will unblock the
request. No other memory requests will be unblocked until the large memory
sufficiently returned.

Because 'Request' and 'Return' are just counters, they can be called as many
times as necessary in whatever sizes are convenient.

When calling 'Request', a process should be sure to request all necessary memory
at once, because if a single process calls 'Request' multiple times before
returning any memory, this can cause a deadlock between multiple processes that
are stuck waiting for more memory before they release memory.

### Worker Subsystem
**Key Files**
 - [worker.go](./worker.go)
 - [workerdownload.go](./workerdownload.go)
 - [workerpool.go](./workerpool.go)
 - [workerupload.go](./workerupload.go)

The worker subsystem is the interface between the renter and the hosts. All
actions (with the exception of some legacy actions that are currently being
updated) that involve working with hosts will pass through the worker subsystem.

#### The Worker Pool

The heart of the worker subsystem is the worker pool, implemented in
[workerpool.go](./workerpool.go). The worker pool contains the set of workers
that can be used to communicate with the hosts, one worker per host. The
function `callWorker` can be used to retrieve a specific worker from the pool,
and the function `callUpdate` can be used to update the set of workers in the
worker pool. `callUpdate` will create new workers for any new contracts, will
update workers for any contracts that changed, and will kill workers for any
contracts that are no longer useful.

##### Inbound Complexities

 - `callUpdate` should be called on the worker pool any time that that the set
   of contracts changes or has updates which would impact what actions a worker
   can take. For example, if a contract's utility changes or if a contract is
   cancelled.
   - `Renter.SetSettings` calls `callUpdate` after changing the settings of the
	 renter. This is probably incorrect, as the actual contract set is updated
	 by the contractor asynchronously, and really `callUpdate` should be
	 triggered by the contractor as the set of hosts is changed.
   - `Renter.threadedDownloadLoop` calls `callUpdate` on each iteration of the
	 outer download loop to ensure that it is always working with the most
	 recent set of hosts. If the contractor is updated to be able to call
	 `callUpdate` during maintenance, this call becomes unnecessary.
   - `Renter.managedRefreshHostsAndWorkers` calls `callUpdate` so that the
	 renter has the latest list of hosts when performing uploads.
	 `Renter.managedRefreshHostsAndWorkers` is itself called in many places,
	 which means there's substantial complexity between the upload subsystem and
	 the worker subsystem. This complexity can be eliminated by having the
	 contractor being responsible for updating the worker pool as it changes the
	 set of hosts, and also by having the worker pool store host map, which is
	 one of the key reasons `Renter.managedRefreshHostsAndWorkers` is called so
	 often - this function returns the set of hosts in addition to updating the
	 worker pool.
 - `callWorker` can be used to fetch a worker and queue work into the worker.
   The worker can be killed after `callWorker` has been called but before the
   returned worker has been used in any way.
   - `renter.BackupsOnHost` will use `callWorker` to retrieve a worker that can
	 be used to pull the backups off of a host.
 - `callWorkers` can be used to fetch the list of workers from the worker pool.
   It should be noted that it is not safe to lock the worker pool, iterate
   through the workers, and then call locking functions on the workers. The
   worker pool must be unlocked if the workers are going to be acquiring locks.
   Which means functions that loop over the list of workers must fetch that list
   separately.

#### The Worker

Each worker in the worker pool is responsible for managing communications with a
single host. The worker has an infinite loop where it checks for work, performs
any outstanding work, and then sleeps for a wake, kill, or shutdown signal. The
implementation for the worker is primarily in [worker.go](./worker.go) and
[workerloop.go](./workerloop.go).

Each type of work that the worker can perform has a queue. A unit of work is
called a job. The worker queue and job structure has been re-written multiple
times, and not every job has been ported yet to the latest structure. But using
the latest structure, you can call `queue.callAdd()` to add a job to a queue.
The worker loop will make all of the decisions around when to execute the job.
Jobs are split into two types, serial and async. Serial jobs are anything that
requires exclusive access to the file contract with the host, the worker will
ensure that only one of these is running at a time. Async jobs are any jobs that
don't require exclusive access to a resource, the worker will run multiple of
these in parallel.

When a worker wakes or otherwise begins the work loop, the worker will check for
each type of work in a specific order, therefore giving certain types of work
priority over other types of work. For example, downloads are given priority
over uploads. When the worker performs a piece of work, it will jump back to the
top of the loop, meaning that a continuous stream of higher priority work can
stall out all lower priority work.

When a worker is killed, the worker is responsible for going through the list of
jobs that have been queued and gracefully terminating the jobs, returning or
signaling errors where appropriate.

[workerjobgeneric.go](./workerjobgeneric.go) and
[workerjobgeneric_test.go](./workerjobgeneric_test.go) contain all of the
generic code and a basic reference implementation for building a job.

##### Inbound Complexities
 - `callQueueDownloadChunk` can be used to schedule a job to participate in a
   chunk download
   - `Renter.managedDistributeDownloadChunkToWorkers` will use this method to
	 issue a brand new download project to all of the workers.
   - `unfinishedDownloadChunk.managedCleanUp` will use this method to re-issue
	 work to workers that are known to have passed on a job previously, but may
	 be required now.
 - `callQueueUploadChunk` can be used to schedule a job to participate in a
   chunk upload
   - `Renter.managedDistributeChunkToWorkers` will use this method to distribute
	 a brand new upload project to all of the workers.
   - `unfinishedUploadChunk.managedNotifyStandbyWorkers` will use this method to
	 re-issue work to workers that are known to have passed on a job previously,
	 but may be required now.

##### Outbound Complexities
 - `managedPerformDownloadChunkJob` is a mess of complexities and needs to be
   refactored to be compliant with the new subsystem format.
 - `managedPerformUploadChunkJob` is a mess of complexities and needs to be
   refactored to be compliant with the new subsystem format.

### Download Subsystem
**Key Files**
 - [download.go](./download.go)
 - [downloadchunk.go](./downloadchunk.go)
 - [downloaddestination.go](./downloaddestination.go)
 - [downloadheap.go](./downloadheap.go)
 - [workerdownload.go](./workerdownload.go)

*TODO* 
  - expand subsystem description

The download code follows a clean/intuitive flow for getting super high and
computationally efficient parallelism on downloads. When a download is
requested, it gets split into its respective chunks (which are downloaded
individually) and then put into the download heap and download history as a
struct of type `download`.

A `download` contains the shared state of a download with all the information
required for workers to complete it, additional information useful to users
and completion functions which are executed upon download completion.

The download history contains a mapping of all of the downloads' UIDs, which
are randomly assigned upon initialization to their corresponding `download`
struct. Unless cleared, users can retrieve information about ongoing and
completed downloads by either retrieving the full history or a specific
download from the history using the API.

The primary purpose of the download heap is to keep downloads on standby
until there is enough memory available to send the downloads off to the
workers. The heap is sorted first by priority, but then a few other criteria
as well.

Some downloads, in particular downloads issued by the repair code, have
already had their memory allocated. These downloads get to skip the heap and
go straight for the workers.

Before we distribute a download to workers, we check the `localPath` of the
file to see if it available on disk. If it is, and `disableLocalFetch` isn't
set, we load the download from disk instead of distributing it to workers.

When a download is distributed to workers, it is given to every single worker
without checking whether that worker is appropriate for the download. Each
worker has their own queue, which is bottlenecked by the fact that a worker
can only process one item at a time. When the worker gets to a download
request, it determines whether it is suited for downloading that particular
file. The criteria it uses include whether or not it has a piece of that
chunk, how many other workers are currently downloading pieces or have
completed pieces for that chunk, and finally things like worker latency and
worker price.

If the worker chooses to download a piece, it will register itself with that
piece, so that other workers know how many workers are downloading each
piece. This keeps everything cleanly coordinated and prevents too many
workers from downloading a given piece, while at the same time you don't need
a giant messy coordinator tracking everything. If a worker chooses not to
download a piece, it will add itself to the list of standby workers, so that
in the event of a failure, the worker can be returned to and used again as a
backup worker. The worker may also decide that it is not suitable at all (for
example, if the worker has recently had some consecutive failures, or if the
worker doesn't have access to a piece of that chunk), in which case it will
mark itself as unavailable to the chunk.

As workers complete, they will release memory and check on the overall state
of the chunk. If some workers fail, they will enlist the standby workers to
pick up the slack.

When the final required piece finishes downloading, the worker who completed
the final piece will spin up a separate thread to decrypt, decode, and write
out the download. That thread will then clean up any remaining resources, and
if this was the final unfinished chunk in the download, it'll mark the
download as complete.

The download process has a slightly complicating factor, which is overdrive
workers. Traditionally, if you need 10 pieces to recover a file, you will use
10 workers. But if you have an overdrive of '2', you will actually use 12
workers, meaning you download 2 more pieces than you need. This means that up
to two of the workers can be slow or fail and the download can still complete
quickly. This complicates resource handling, because not all memory can be
released as soon as a download completes - there may be overdrive workers
still out fetching the file. To handle this, a catchall 'cleanUp' function is
used which gets called every time a worker finishes, and every time recovery
completes. The result is that memory gets cleaned up as required, and no
overarching coordination is needed between the overdrive workers (who do not
even know that they are overdrive workers) and the recovery function.

By default, the download code organizes itself around having maximum possible
throughput. That is, it is highly parallel, and exploits that parallelism as
efficiently and effectively as possible. The hostdb does a good job of selecting
for hosts that have good traits, so we can generally assume that every host
or worker at our disposable is reasonably effective in all dimensions, and
that the overall selection is generally geared towards the user's
preferences.

We can leverage the standby workers in each unfinishedDownloadChunk to
emphasize various traits. For example, if we want to prioritize latency,
we'll put a filter in the 'managedProcessDownloadChunk' function that has a
worker go standby instead of accept a chunk if the latency is higher than the
targeted latency. These filters can target other traits as well, such as
price and total throughput.

### Download Streaming Subsystem
**Key Files**
 - [downloadstreamer.go](./downloadstreamer.go)

*TODO* 
  - fill out subsystem explanation

### Skyfile Subsystem
**Key Files**
 - [skyfile.go](./skyfile.go)
 - [skyfilefanout.go](./skyfilefanout.go)
 - [skyfilefanoutfetch.go](./skyfilefanoutfetch.go)

The skyfile system contains methods for encoding, decoding, uploading, and
downloading skyfiles using Skylinks, and is one of the foundations underpinning
Skynet.

The skyfile format is a custom format which prepends metadata to a file such
that the entire file and all associated metadata can be recovered knowing
nothing more than a single sector root. That single sector root can be encoded
alongside some compressed fetch offset and length information to create a
skylink.

**Outbound Complexities**
 - callUploadStreamFromReader is used to upload new data to the Uplo network when
   creating skyfiles. This call appears three times in
   [skyfile.go](./skyfile.go)

### Stream Buffer Subsystem
**Key Files**
 - [streambuffer.go](./streambuffer.go)
 - [streambufferlru.go](./streambufferlru.go)

The stream buffer subsystem coordinates buffering for a set of streams. Each
stream has an LRU which includes both the recently visited data as well as data
that is being buffered in front of the current read position. The LRU is
implemented in [streambufferlru.go](./streambufferlru.go).

If there are multiple streams open from the same data source at once, they will
share their cache. Each stream will maintain its own LRU, but the data is stored
in a common stream buffer. The stream buffers draw their data from a data source
interface, which allows multiple different types of data sources to use the
stream buffer.

### Upload Subsystem
**Key Files**
 - [directoryheap.go](./directoryheap.go)
 - [upload.go](./upload.go)
 - [uploadheap.go](./uploadheap.go)
 - [uploadchunk.go](./uploadchunk.go)
 - [workerupload.go](./workerupload.go)

*TODO* 
  - expand subsystem description

The Renter uploads `uplofiles` in 40MB chunks. Redundancy kept at the chunk level
which means each chunk will then be split in `datapieces` number of pieces. For
example, a 10/20 scheme would mean that each 40MB chunk will be split into 10
4MB pieces, which is turn will be uploaded to 30 different hosts (10 data pieces
and 20 parity pieces).

Chunks are uploaded by first distributing the chunk to the worker pool. The
chunk is distributed to the worker pool by adding it to the upload queue and
then signalling the worker upload channel. Workers that are waiting for work
will receive this channel and begin the upload. First the worker creates a
connection with the host by creating an `editor`. Next the `editor` is used to
update the file contract with the next data being uploaded. This will update the
merkle root and the contract revision.

**Outbound Complexities**  
 - The upload subsystem calls `callThreadedBubbleMetadata` from the Health Loop
   to update the filesystem of the new upload
 - `Upload` calls `callBuildAndPushChunks` to add upload chunks to the
   `uploadHeap` and then signals the heap's `newUploads` channel so that the
   Repair Loop will work through the heap and upload the chunks

### Download By Root Subsystem
**Key Files**
 - [projectdownloadbyroot.go](./projectdownloadbyroot.go)
 - [workerdownloadbyroot.go](./workerdownloadbyroot.go)

The download by root subsystem exports a single method that allows a caller to
download or partially download a sector from the Uplo network knowing only the
Merkle root of that sector, and not necessarily knowing which host on the
network has that sector. The single exported method is 'DownloadByRoot'.

This subsystem was created primarily as a facilitator for the skylinks of
Skynet. Skylinks provide a merkle root and some offset+length information, but
do not provide any information about which hosts are storing the sectors. The
exported method of this subsystem will primarily be called by skylink methods,
as opposed to being used directly by external users.

### Upload Streaming Subsystem
**Key Files**
 - [uploadstreamer.go](./uploadstreamer.go)

*TODO* 
  - fill out subsystem explanation

**Inbound Complexities**
 - The skyfile subsystem makes three calls to `callUploadStreamFromReader()` in
   [skyfile.go](./skyfile.go)
 - The snapshot subsystem makes a call to `callUploadStreamFromReader()`

### Health and Repair Subsystem
**Key Files**
 - [metadata.go](./metadata.go)
 - [repair.go](./repair.go)
 - [stuckstack.go](./stuckstack.go)
 - [uploadheap.go](./uploadheap.go)

*TODO*
  - Update naming of bubble methods to updateAggregateMetadata, this will more
    closely match the file naming as well. Update the health loop description to
    match new naming
  - Move HealthLoop and related methods out of repair.go to health.go
  - Pull out repair code from  uploadheap.go so that uploadheap.go is only heap
    related code. Put in repair.go
  - Pull out stuck loop code from uploadheap.go and put in repair.go
  - Review naming of files associated with this subsystem
  - Create benchmark for health loop and add print outs to Health Loop section
  - Break out Health, Repair, and Stuck code into 3 distinct subsystems
  
There are 3 main functions that work together to make up Uplo's file repair
mechanism, `threadedUpdateRenterHealth`, `threadedUploadAndRepairLoop`, and
`threadedStuckFileLoop`. These 3 functions will be referred to as the health
loop, the repair loop, and the stuck loop respectively.

The Health and Repair subsystem operates by scanning aggregate information kept
in each directory's metadata. An example of this metadata would be the aggregate
filesystem health. Each directory has a field `AggregateHealth` which represents
the worst aggregate health of any file or subdirectory in the directory. Because
the field is recursive, the `AggregateHealth` of the root directory represents
the worst health of any file in the entire filesystem. Health is defined as the
percent of redundancy missing, this means that a health of 0 is a full health
file.

`threadedUpdateRenterHealth` is responsible for keeping the aggregate
information up to date, while the other two loops use that information to decide
what upload and repair actions need to be performed.

#### Health Loops
The health loop is responsible for ensuring that the health of the renter's file
directory is updated periodically. Along with the health, the metadata for the
files and directories is also updated. 

One of the key directory metadata fields that the health loop uses is
`LastHealthCheckTime` and `AggregateLastHealthCheckTime`. `LastHealthCheckTime`
is the timestamp of when a directory or file last had its health re-calculated
during a bubble call. When determining which directory to start with when
updating the renter's file system, the health loop follows the path of oldest
`AggregateLastHealthCheckTime` to find the directory  or sub tree that is the
most out of date. To do this, the health loop uses
`managedOldestHealthCheckTime`. This method starts at the root level of the
renter's file system and begins checking the `AggregateLastHealthCheckTime` of
the subdirectories. It then finds which one is the oldest and moves into that
subdirectory and continues the search.  Once it reaches a directory that either
has no subdirectories, or the current directory  has an older
`AggregateLastHealthCheckTime` than any of the subdirectories, or it has found
a reasonably sized sub tree defined by the health loop constants, it returns
that timestamp and the UploPath of the directory.

Once the health loop has found the most out of date directory or sub tree, it calls
`managedBubbleMetadata`, to be referred to as bubble, on that directory. When a
directory is bubbled, the metadata information is recalculated and saved to disk
and then bubble is called on the parent directory until the top level directory
is reached. During this calculation, every file in the directory is opened,
modified, and fsync'd individually. See benchmark results:

*TODO* - add benchmark 

If during a bubble a file is found that meets the threshold health
for repair, then a signal is sent to the repair loop. If a stuck chunk is found
then a signal is sent to the stuck loop. Once the entire renter's directory has
been updated within the healthCheckInterval the health loop sleeps until the
time interval has passed.

Since we are updating the metadata on disk during the bubble calls we want to
ensure that only one bubble is being called on a directory at a time. We do this
through `managedPrepareBubble` and `managedCompleteBubbleUpdate`. The renter has
a `bubbleUpdates` field that tracks all the bubbles and the `bubbleStatus`.
Bubbles can either be active or pending. When bubble is called on a directory,
`managedPrepareBubble` will check to see if there are any active or pending
bubbles for the directory. If there are no bubbles being tracked for that
directory then an active bubble update is added to the renter for the directory
and the bubble is executed immediately. If there is a bubble currently being
tracked for the directory then the bubble status is set to pending and the
bubble is not executed immediately. Once a bubble is finished it will call
`managedCompleteBubbleUpdate` which will check the status of the bubble. If the
status is an active bubble then it is removed from the renter's tracking. If the
status was a pending bubble then the status is set to active and bubble is
called on the directory again. 

**Inbound Complexities**  
 - The Repair loop relies on Health Loop and `callThreadedBubbleMetadata` to
   keep the filesystem accurately updated in order to work through the file
   system in the correct order.
 - `DeleteFile` calls `callThreadedBubbleMetadata` after the file is deleted
 - `RenameFile` calls `callThreadedBubbleMetadata` on the current and new
   directories when a file is renamed
 - The upload subsystem calls `callThreadedBubbleMetadata` from the Health Loop
   to update the filesystem of the new upload

**Outbound Complexities**   
 - The Health Loop triggers the Repair Loop when unhealthy files are found. This
   is done by `managedPerformBubbleMetadata` signaling the
   `r.uploadHeap.repairNeeded` channel when it is at the root directory and the
   `AggregateHealth` is above the `RepairThreshold`.
 - The Health Loop triggers the Stuck Loop when stuck files are found. This is
   done by `managedPerformBubbleMetadata` signaling the
   `r.uploadHeap.stuckChunkFound` channel when it is at the root directory and
   `AggregateNumStuckChunks` is greater than zero.

#### Repair Loop
The repair loop is responsible for uploading new files to the renter and
repairing existing files. The heart of the repair loop is
`threadedUploadAndRepair`, a thread that continually checks for work, schedules
work, and then updates the filesystem when work is completed.

The renter tracks backups and uplofiles separately, which essentially means the
renter has a backup filesystem and a uplofile filesystem. As such, we need to
check both these filesystems separately with the repair loop. Since the backups
are in a different filesystem, the health loop does not check on the backups
which means that there are no outside triggers for the repair loop that a backup
wasn't uploaded successfully and needs to be repaired. Because of this we always
check for backup chunks first to ensure backups are succeeding. There is a size
limit on the heap to help check memory usage in check, so by adding backup
chunks to the heap first we ensure that we are never skipping over backup chunks
due to a full heap.

For the uplofile filesystem the repair loop uses a directory heap to prioritize
which chunks to add. The directoryHeap is a max heap of directory elements
sorted by health. The directory heap is initialized by pushing an unexplored
root directory element. As directory elements are popped of the heap, they are
explored, which means the directory that was popped off the heap as unexplored
gets marked as explored and added back to the heap, while all the subdirectories
are added as unexplored. Each directory element contains the health information
of the directory it represents, both directory health and aggregate health. If a
directory is unexplored the aggregate health is considered, if the directory is
explored the directory health is consider in the sorting of the heap. This is to
allow us to navigate through the filesystem and follow the path of worse health
to find the most in need directories first. When the renter needs chunks to add
to the upload heap, directory elements are popped of the heap and chunks are
pulled from that directory to be added to the upload heap. If all the chunks
that need repairing are added to the upload heap then the directory element is
dropped. If not all the chunks that need repair are added, then the directory
element is added back to the directory heap with a health equal to the next
chunk that would have been added, thus re-prioritizing that directory in the
heap.

To build the upload heap for the uplofile filesystem, the repair loop checks if
the file system is healthy by checking the top directory element in the
directory heap. If healthy and there are no chunks currently in the upload heap,
then the repair loop sleeps until it is triggered by a new upload or a repair is
needed. If the filesystem is in need of repair, chunks are added to the upload
heap by popping the directory off the directory heap and adding any chunks that
are a worse health than the next directory in the directory heap. This continues
until the `MaxUploadHeapChunks` is met. The repair loop will then repair those
chunks and call bubble on the directories that chunks were added from to keep
the file system updated. This will continue until the file system is healthy,
which means all files have a health less than the `RepairThreshold`.

When repairing chunks, the Renter will first try and repair the chunk from the
local file on disk. If the local file is not present, the Renter will download
the needed data from its contracts in order to perform the repair. In order for
a remote repair, ie repairing from data downloaded from the Renter's contracts,
to be successful the chunk must be at 1x redundancy or better. If a chunk is
below 1x redundancy and the local file is not present the chunk, and therefore
the file, is considered lost as there is no way to repair it. 

**NOTE:** if the repair loop does not find a local file on disk, it will reset
the localpath of the uplofile to an empty string. This is done to avoid the
uplofile being corrupted in the future by a different file being placed on disk
at the original localpath location.

**Inbound Complexities**  
 - `Upload` adds chunks directly to the upload heap by calling
   `callBuildAndPushChunks`
 - Repair loop will sleep until work is needed meaning other threads will wake
   up the repair loop by calling the `repairNeeded` channel
 - There is always enough space in the heap, or the number of backup chunks is
   few enough that all the backup chunks are always added to the upload heap.
 - Stuck chunks get added directly to the upload heap and have priority over
   normal uploads and repairs
 - Streaming upload chunks are added directory to the upload heap and have the
   highest priority

**Outbound Complexities**  
 - The Repair loop relies on Health Loop and `callThreadedBubbleMetadata` to
   keep the filesystem accurately updated in order to work through the file
   system in the correct order.
 - The repair loop passes chunks on to the upload subsystem and expects that
   subsystem to handle the request 
 - `Upload` calls `callBuildAndPushChunks` to add upload chunks to the
   `uploadHeap` and then signals the heap's `newUploads` channel so that the
   Repair Loop will work through the heap and upload the chunks

#### Stuck Loop
File's are marked as `stuck` if the Renter is unable to fully upload the file.
While there are many reasons a file might not be fully uploaded, failed uploads
due to the Renter, ie the Renter shut down, will not cause the file to be marked
as `stuck`. The goal is to mark a chunk as stuck if it is independently unable
to be uploaded. Meaning, this chunk is unable to be repaired but other chunks
are able to be repaired. We mark a chunk as stuck so that the repair loop will
ignore it in the future and instead focus on chunks that are able to be
repaired.

The stuck loop is responsible for targeting chunks that didn't get repaired
properly. There are two methods for adding stuck chunks to the upload heap, the
first method is random selection and the second is using the `stuckStack`. On
start up the `stuckStack` is empty so the stuck loop begins using the random
selection method. Once the `stuckStack` begins to fill, the stuck loop will use
the `stuckStack` first before using the random method.

For the random selection one chunk is selected uniformly at random out of all of
the stuck chunks in the filesystem. The stuck loop does this by first selecting
a directory containing stuck chunks by calling `managedStuckDirectory`. Then
`managedBuildAndPushRandomChunk` is called to select a file with stuck chunks to
then add one stuck chunk from that file to the heap. The stuck loop repeats this
process of finding a stuck chunk until there are `maxRandomStuckChunksInHeap`
stuck chunks in the upload heap or it has added `maxRandomStuckChunksAddToHeap`
stuck chunks to the upload heap. Stuck chunks are priority in the heap, so
limiting it to `maxStuckChunksInHeap` at a time prevents the heap from being
saturated with stuck chunks that potentially cannot be repaired which would
cause no other files to be repaired. 

For the stuck loop to begin using the `stuckStack` there needs to have been
successful stuck chunk repairs. If the repair of a stuck chunk is successful,
the UploPath of the UploFile it came from is added to the Renter's `stuckStack`
and a signal is sent to the stuck loop so that another stuck chunk can added to
the heap. The repair loop with continue to add stuck chunks from the
`stuckStack` until there are `maxStuckChunksInHeap` stuck chunks in the upload
heap. Stuck chunks added from the `stuckStack` will have priority over random
stuck chunks, this is determined by setting the `fileRecentlySuccessful` field
to true for the chunk. The `stuckStack` tracks `maxSuccessfulStuckRepairFiles`
number of UploFiles that have had stuck chunks successfully repaired in a LIFO
stack. If the LIFO stack already has `maxSuccessfulStuckRepairFiles` in it, when
a new UploFile is pushed onto the stack the oldest UploFile is dropped from the
stack so the new UploFile can be added. Additionally, if UploFile is being added
that is already being tracked, then the original reference is removed and the
UploFile is added to the top of the Stack. If there have been successful stuck
chunk repairs, the stuck loop will try and add additional stuck chunks from
these files first before trying to add a random stuck chunk. The idea being that
since all the chunks in a UploFile have the same redundancy settings and were
presumably uploaded around the same time, if one chunk was able to be repaired,
the other chunks should be able to be repaired as well. Additionally, the reason
a LIFO stack is used is because the more recent a success was the higher
confidence we have for additional successes.

If the repair wasn't successful, the stuck loop will wait for the
`repairStuckChunkInterval` to pass and then try another random stuck chunk. If
the stuck loop doesn't find any stuck chunks, it will sleep until a bubble wakes
it up by finding a stuck chunk.

**Inbound Complexities**  
 - Chunk repair code signals the stuck loop when a stuck chunk is successfully
   repaired
 - Health loop signals the stuck loop when aggregateNumStuckChunks for the root
   directory is > 0

**State Complexities**  
 - The stuck loop and the repair loop use a number of the same methods when
   building `unfinishedUploadChunks` to add to the `uploadHeap`. These methods
   rely on the `repairTarget` to know if they should target stuck chunks or
   unstuck chunks 

**TODOs**  
 - once bubbling metadata has been updated to be more I/O efficient this code
   should be removed and we should call bubble when we clean up the upload chunk
   after a successful repair.

### Backup Subsystem
**Key Files**
 - [backup.go](./backup.go)
 - [backupsnapshot.go](./backupsnapshot.go)

*TODO* 
  - expand subsystem description

The backup subsystem of the renter is responsible for creating local and remote
backups of the user's data, such that all data is able to be recovered onto a
new machine should the current machine + metadata be lost.

### Refresh Paths Subsystem
**Key Files**
 - [refreshpaths.go](./refreshpaths.go)

The refresh paths subsystem of the renter is a helper subsystem that tracks the
minimum unique paths that need to be refreshed in order to refresh the entire
affected portion of the file system.

**Inbound Complexities** 
 - `callAdd` is used to try and add a new path. 
 - `callRefreshAll` is used to refresh all the directories corresponding to the
   unique paths in order to update the filesystem
