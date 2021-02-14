Developer Environment
=====================

Uplo is written in Go. To build and test Uplo, you are going to need a working go
environment, including having both `$GOROOT/bin` and `$GOPATH/bin` in your
`$PATH`. For most Linux distributions, Go will be in the package manager,
though it may be an old version that is incompatible with Uplo. Once you have a
working Go environment, you are set to build the project. If you plan on cross
compiling Uplo, you may need to install Go from source. You can find information
on that [here](http://golang.org/doc/install/source).

Uplo has a development build, an automated testing build, and a release
build. The release build is the only one that can synchronize to the full
network. To get the release build, it is usually sufficient to run `go get -u
github.com/uplo-tech/uplo/...`. This will download Uplo and its dependencies
and install binaries in `$GOPATH/bin`.

After downloading, you can find the Uplo source code in
`$GOPATH/src/github.com/uplo-tech/uplo`. To build the release binary, run
`make release` from this directory. To build the release binary with a (slow)
race detector and an array of debugging asserts, run `make release-race`. To
build the developer binary (which has a different genesis block, faster block
times, and a few other tweaks), run `make dev`.

If you intend to contribute to Uplo, you should start by forking the project on
GitLab, and then adding your fork as a "remote" in the Uplo git repository via
`git remote add [fork name] [fork url]`. Now you can develop by pulling changes
from `origin`, pushing your modifications to `[fork name]`, and then making a
merge request on GitLab.


Developer Conventions
=====================

This file is meant to help a developer navigate the codebase and develop clean,
maintainable code. Knowing all of these conventions will also make it easier to
read and code review the Uplo project.

The primary purpose of the conventions within Uplo is to keep the codebase
simple. Simpler constructions means easier code reviews, greater accessibility
to newcomers, and less potential for mistakes. It is also to keep things
uniform, much in the spirit of `go fmt`. When everything looks the same,
everyone has an easier time reading and reviewing code they did not write
themselves.

Documentation
-------------

All structs, functions, and interfaces must have a docstring.

Anytime that something is left unfinished, place a comment containing the string
'TODO:'. This sends a clear message to other developers, and creates a greppable
way to find unfinished parts of the codebase. 'TODO' statements are currently
discouraged.  As the codebase matures, 'TODO' statements will become
increasingly frowned upon. 'TODO' statements should not document feature
requests, but instead document incompleteness where the incompleteness causes
disruption to user experience or causes a security vulnerability. Any TODOs
added to the code base should be documented in the following places:
1. At the exact spot in the file where the implementation belongs
2. At the top of the relevant file
3. In the README file of the relevant module

Documentation should give a sense of what each function does, but should also
give a sense of the overall architecture of the code. Where useful, examples
should be provided, and common pitfalls should be explained. Anything that
breaks other conventions in any way needs to have a comment, even if it is
obvious why the convention had to be broken.

The goal of the codebase is to be accessible to newbies. Anything more advanced
than what you would expect to remember from an 'Intro to Data Structures' class
should have an explanation about what the concept it is and why it was picked
over other potential choices.

Code that exists purely to be compatible with previous versions of the
software should be tagged with a `COMPATvX.X.X` comment. Examples below.

```go
// Find and sort the outputs.
outputs := getOutputs()
// TODO: actually sort the outputs.
```

```go
// Disallow unknown agents.
//
// COMPATv0.4.0: allow a blank agent to preserve compatibility with
// 'uploc' v0.4.0, which did not set an agent.
if agent != "UploAgent" && agent != "" {
	return errors.New("unrecognized agent!")
}
```

### Module READMEs
Below is the standard format for the README files for the modules.

># Modules Name
>Module overview.
>
>## Subsystems
>List subsystems
> - Subsystem 1
> - Subsystem 2
> - etc..
>
>### Subsystem 1
>**Key Files**  
>List files
> - file1.go
> - file2.go
> - etc..
>
>Description of Subsystem
>
>**Exports**  
>List Exported methods
>
>**Inbound Complexities**  
>List any interactions with other subsystems that have dependencies on this subsystem.
>
>**Outbound Complexities**  
>List any interactions with other subsystems that this subsystem has dependencies on.
>
>**TODOs**  
>List any outstanding TODOs for the subsystem
>

Naming
------

Names are used to give readers and reviewers a sense of what is happening in
the code. When naming variables, you should assume that the person reading your
code is unfamiliar with the codebase. Short names (like `cs` instead of
`consensusSet`) should only be used when the context is immediately obvious.
For example `cs := new(ConsensusSet)` is immediately obvious context for `cs`,
and so `cs` is appropriate for the rest of the function.

Data structures should never have shortened names. `FileContract.mr` is
confusing to anyone who has not used the data structure extensively. The code
should be accessible to people who are unfamiliar with the codebase. One
exception is for the variable called `mu`, which is short for 'mutex'. This
exception is made because `mu` appears in many data structures.

When calling functions with obscure parameters, named variables should be used
to indicate what the parameters do. For example, `m := NewMiner(1)` is
confusing. Instead, use `threads := 1; m := NewMiner(threads)`. The name gives
readers a sense of what the parameter within `NewMiner` does even when they are
not familiar with the `NewMiner` function. Where possible, functions with
obscure, untyped inputs should be avoided.

The most important thing to remember when choosing names is to cater to people
who are unfamiliar with the code. A reader should never have to ask 'What is
`cs`?' on their first pass through the code, even though to experienced
developers it is obvious that `cs` refers to a `consensus.ConsensusSet`.

### Function Prefixes

Uplo uses special prefixes for certain functions to hint about their usage to the
caller.

#### `threaded`

Prefix functions with `threaded` (e.g., `threadedMine`) to indicate that callers
should only call these functions within their own goroutine (e.g.,
`go threadedMine()`). These functions must manage their own thread-safety.

#### `managed`

Prefix functions with `managed` (e.g. `managedUpdateWorkerPool`) if the function
acquires any locks in its body.

### Exported Functions

Exported functions should utilize the module's thread group. This ensures that
if the module is stopped than any exported methods that are called will return
an appropriate error. This is important for export methods that are either
returning a status that another modules will act on or performing a action that
will alter the module in some way.

File Ordering
-------------

Each tracked file in the repository should be laid out in the following order.

1. Vars
2. Types
3. Functions
4. Methods

Ordering should be: category (vars, types, etc) -> exported then non-exported 
-> alphabetical

Control Flow
------------

Where possible, control structures should be minimized or avoided. This
includes avoiding nested if statements, and avoiding else statements where
possible. Sometimes, complex control structures are necessary, but where
possible use alternative code patterns and insert functions to break things up.

Example:

```go
// Do not do this:
if err != nil {
	return
} else {
	forkBlockchain(node)
}

// Instead do this:
if err != nil {
	return
}
forkBlockchain(node)
```

Mutexes
-------

All exported functions from a package and/or object need to be thread safe.
Usually, this means that the first lines of the function contain a `Lock();
defer Unlock()`. Simple locking schemes should be preferred over performant
locking schemes. As will everything else, anything unusual or convention
breaking should have a comment.

Non-exported functions should not do any locking unless they are named with the
proper prefix (see [Function Prefixes](#function-prefixes)). The responsibility
for thread-safety comes from the exported functions which call the non-exported
functions. Maintaining this convention minimizes developer overhead when working
with complex objects.

Our concurrency model is such that modules should never call out to higher level
packages while under a lock.  For example, the `contractor` module should never
call the `renter` module while holding a `Lock()`.

Error Handling
--------------

All errors need to be checked as soon as they are received, even if they are
known to not cause problems. The statement that checks the error needs to be
`if err != nil`, and if there is a good reason to use an alternative statement
(such as `err == nil`), it must be documented. The body of the if statement
should be at most 4 lines, but usually only one. Anything requiring more lines
needs to be its own function.

Example:

```go
block, err := s.AcceptBlock()
if err != nil {
	handleAcceptBlockErr(block, err)
	return
}
```

Sanity Checks
-------------

Some functions make assumptions. For example, the `addTransaction` function
assumes that the transaction being added is not in conflict with any other
transactions. Where possible, these explicit assumptions should be validated.

Example:

```go
if build.DEBUG {
	_, exists := tp.usedOutputs[input.OutputID]
	if exists {
		panic("incorrect use of addTransaction")
	}
}
```

In the example, a panic is called for incorrect use of the function, but only
in debug mode. This failure will be invisible in production code, but the code
will have higher performance because the code should never fail anyway.

If the code is continually checking items that should be universally true,
mistakes are easier to catch during testing, and side effects are less likely
to go unnoticed.

Sanity checks and panics are purely to check for developer mistakes. A user
should not be able to trigger a panic, and no set of network communications or
real-world conditions should be able to trigger a panic.

Testing
-------

The test suite code should be the same quality as the rest of the codebase.
When writing new code in a pull request, the pull request should include test
coverage for the code.

Most modules have a tester object, which can be created by calling
`createXXXTester`. Module testers typically have a consensus set, a miner, a
wallet, and a few other relevant modules that can be used to build
transactions, mine blocks, etc.

In general, testing that uses exclusively exported functions to achieve full
coverage is preferred. These types of tests seem to find more bugs and trigger
more asserts.

Any testing provided by a third party which is both maintainable and reasonably
quick will be accepted. There is little downside to more testing, even when the
testing is largely redundant.
