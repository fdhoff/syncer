/*
syncer -- stateful file/device data syncer.
Copyright (C) 2015 Sergey Matveev <stargrave@stargrave.org>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

// Stateful file/device data syncer.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime"

	"github.com/dchest/blake2b"
)

var (
	blkSize   = flag.Int64("blk", 2*1<<10, "Block size (KiB)")
	statePath = flag.String("state", "state.bin", "Path to statefile")
	dstPath   = flag.String("dst", "/dev/ada0", "Path to destination disk")
	srcPath   = flag.String("src", "/dev/da0", "Path to source disk")
)

type SyncEvent struct {
	i    int64
	buf  []byte
	data []byte
}

func prn(s string) {
	os.Stdout.Write([]byte(s))
	os.Stdout.Sync()
}

func main() {
	flag.Parse()
	bs := *blkSize * int64(1<<10)

	// Open source, calculate number of blocks
	var size int64
	src, err := os.Open(*srcPath)
	if err != nil {
		log.Fatalln("Unable to open src:", err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		log.Fatalln("Unable to read src stat:", err)
	}
	if fi.Mode()&os.ModeDevice == os.ModeDevice {
		size, err = src.Seek(0, 2)
		if err != nil {
			log.Fatalln("Unable to seek src:", err)
		}
		src.Seek(0, 0)
	} else {
		size = fi.Size()
	}
	blocks := size / bs
	if size%bs != 0 {
		blocks++
	}
	log.Println(blocks, bs, "byte blocks")

	// Open destination
	dst, err := os.OpenFile(*dstPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatalln("Unable to open dst:", err)
	}
	defer dst.Close()

	// Check if we already have statefile and read the state//
	state := make([]byte, blake2b.Size*blocks)
	var i int64
	var tmp []byte
	if _, err := os.Stat(*statePath); err == nil {
		log.Println("State file found")
		stateFile, err := os.Open(*statePath)
		if err != nil {
			log.Fatalln("Unable to read statefile:", err)
		}

		// Check previously used size and block size
		tmp = make([]byte, 8)
		n, err := stateFile.Read(tmp)
		if err != nil || n != 8 {
			log.Fatalln("Invalid statefile")
		}
		prevSize := int64(binary.BigEndian.Uint64(tmp))
		if size != prevSize {
			log.Fatalln(
				"Size differs with state file:",
				prevSize, "instead of", size,
			)
		}
		tmp = make([]byte, 8)
		n, err = stateFile.Read(tmp)
		if err != nil || n != 8 {
			log.Fatalln("Invalid statefile")
		}
		prevBs := int64(binary.BigEndian.Uint64(tmp))
		if bs != prevBs {
			log.Fatalln(
				"Blocksize differs with state file:",
				prevBs, "instead of", bs,
			)
		}

		n, err = stateFile.Read(state)
		if err != nil || n != len(state) {
			log.Fatalln("Corrupted statefile")
		}
		stateFile.Close()
	}
	stateFile, err := ioutil.TempFile(".", "syncer")
	if err != nil {
		log.Fatalln("Unable to create temporary file:", err)
	}
	tmp = make([]byte, 8)
	binary.BigEndian.PutUint64(tmp, uint64(size))
	stateFile.Write(tmp)
	tmp = make([]byte, 8)
	binary.BigEndian.PutUint64(tmp, uint64(bs))
	stateFile.Write(tmp)

	// Create buffers and event channel
	workers := runtime.NumCPU()
	log.Println(workers, "workers")
	bufs := make(chan []byte, workers)
	for i := 0; i < workers; i++ {
		bufs <- make([]byte, int(bs))
	}
	syncs := make(chan chan SyncEvent, workers)

	// Writer
	prn("[")
	finished := make(chan struct{})
	go func() {
		var event SyncEvent
		for sync := range syncs {
			event = <-sync
			if event.data != nil {
				dst.Seek(event.i*bs, 0)
				dst.Write(event.data)
			}
			bufs <- event.buf
			<-sync
		}
		close(finished)
	}()

	// Reader
	for i = 0; i < blocks; i++ {
		buf := <-bufs
		n, err := src.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Fatalln("Error during src read:", err)
			}
			break
		}
		sync := make(chan SyncEvent)
		syncs <- sync
		go func(i int64) {
			sum := blake2b.Sum512(buf[:n])
			sumState := state[i*blake2b.Size : i*blake2b.Size+blake2b.Size]
			if bytes.Compare(sumState, sum[:]) != 0 {
				sync <- SyncEvent{i, buf, buf[:n]}
				prn("%")
			} else {
				sync <- SyncEvent{i, buf, nil}
				prn(".")
			}
			copy(sumState, sum[:])
			close(sync)
		}(i)
	}
	close(syncs)
	<-finished
	prn("]\n")

	log.Println("Saving state")
	stateFile.Write(state)
	stateFile.Close()
	if err = os.Rename(stateFile.Name(), *statePath); err != nil {
		log.Fatalln(
			"Unable to overwrite statefile:", err,
			"saved state is in:", stateFile.Name(),
		)
	}
}
