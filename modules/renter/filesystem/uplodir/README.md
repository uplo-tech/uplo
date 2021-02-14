# uplodir
The uplodir module is responsible for creating and maintaining the directory
metadata information stored in the `.uplodir` files on disk. This includes all
disk interaction and metadata definition. These uplodirs represent directories on
the Uplo network.

## Structure of the uplodir
The uplodir is a dir on the Uplo network and the uplodir metadata is a JSON
formatted metadata file that contains aggregate and non-aggregate fields. The
aggregate fields are the totals of the uplodir and any sub uplodirs, or are
calculated based on all the values in the subtree. The non-aggregate fields are
information specific to the uplodir that is not an aggregate of the entire sub
directory tree

## Subsystems
The following subsystems help the uplodir module execute its responsibilities:
 - [Persistence Subsystem](#persistence-subsystem)
 - [File Format Subsystem](#file-format-subsystem)
 - [uplodirSet Subsystem](#uplodirset-subsystem)
 - [DirReader Subsystem](#dirreader-subsystem)

 ### Persistence Subsystem
 **Key Files**
- [persist.go](./persist.go)
- [persistwal.go](./persistwal.go)

The Persistence subsystem is responsible for the disk interaction with the
`.uplodir` files and ensuring safe and performant ACID operations by using the
[writeaheadlog](https://github.com/uplo-tech/writeaheadlog) package. There
are two WAL updates that are used, deletion and metadata updates.

The WAL deletion update removes all the contents of the directory including the
directory itself.

The WAL metadata update re-writes the entire metadata, which is stored as JSON.
This is used whenever the metadata changes and needs to be saved as well as when
a new uplodir is created.

**Exports**
 - `ApplyUpdates`
 - `CreateAndApplyTransaction`
 - `IsuplodirUpdate`
 - `New`
 - `Loaduplodir`
 - `UpdateMetadata`

**Inbound Complexities**
 - `callDelete` deletes a uplodir from disk
    - `uplodirSet.Delete` uses `callDelete`
 - `Loaduplodir` loads a uplodir from disk
    - `uplodirSet.open` uses `Loaduplodir`

### File Format Subsystem
 **Key Files**
- [uplodir.go](./uplodir.go)

The file format subsystem contains the type definitions for the uplodir
format and methods that return information about the uplodir.

**Exports**
 - `Deleted`
 - `Metatdata`
 - `UploPath`

### uplodirSet Subsystem
 **Key Files**
- [uplodirset.go](./uplodirset.go)

A uplodir object is threadsafe by itself, and to ensure that when a uplodir is
accessed by multiple threads that it is still threadsafe, uplodirs should always
be accessed through the uplodirSet. The uplodirSet was created as a pool of
uplodirs which is used by other packages to get access to uplodirEntries which are
wrappers for uplodirs containing some extra information about how many threads
are using it at a certain time. If a uplodir was already loaded the uplodirSet
will hand out the existing object, otherwise it will try to load it from disk.

**Exports**
 - `HealthPercentage`
 - `NewuplodirSet`
 - `Close`
 - `Delete`
 - `DirInfo`
 - `DirList`
 - `Exists`
 - `InitRootDir`
 - `Newuplodir`
 - `Open`
 - `Rename`

**Outbound Complexities**
 - `Delete` will use `callDelete` to delete the uplodir once it has been acquired
   in the set
 - `open` calls `Loaduplodir` to load the uplodir from disk

### DirReader Subsystem
**Key Files**
 - [dirreader.go](./dirreader.go)

The DirReader Subsystem creates the DirReader which is used as a helper to read
raw .uplodir from disk

**Exports**
 - `Close`
 - `Read`
 - `Stat`
 - `DirReader`
