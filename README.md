syncer
======

Fast stateful file/disk data syncer.

#### Description

The main purpose of this utility is fast data synchronizing between two
hard drives: one is fast (SSD, SATA HDD), another is connected through
slow USB interface. Task is to lower amounts of data needed to be
transferred.

This utility is stateful: it keeps precomputed data hashes in separate
statefile and use it to determine if we need to update block of data.

```
# sync from very fast SSD to slow USB connected HDD
% ./syncer -src /dev/ada0 -dst /dev/da0 -state state.bin
[%%%%%%]
# all blocks were transferred to da0
```

Now we have statefile containing cryptographic hashes of the blocks from
source and copy of all read data in destination. Now if we run it again:

```
% ./syncer -src /dev/ada0 -dst /dev/da0 -state state.bin
[....%.]
# only one block was transferred to da0
```

Only one modified block was transferred during this session. We read all
data from source again, compute hashes and understand what was updated
since the last run. Statefile is updated at the end.

Utility parallelize hash computations among all found CPUs. It updates
statefile atomically (saves data in temporary file and then renames it).
You can configure the blocksize: shorter transfers but bigger statefile
(it is kept in memory), or larger transfer and smaller statefile. All
writes are sequential.

syncer is free software: see the file COPYING for copying conditions.

### Installation

```
% go get github.com/dchest/blake2b
% go build
# syncer executable file should be in current directory
```

### Statefile Format

`SRC_SIZE || BLK_SIZE || HASH0 || HASH1 || ...`

SRC_SIZE contains size of the source, when it was firstly read. BLK_SIZE
is the blocksize used. Both is 64-bit big-endian unsigned integers. If
either size or blocksize differs, then syncer will deny using that
statefile as a precaution. HASHx is BLAKE2b-512 hash output, 64 bytes.
