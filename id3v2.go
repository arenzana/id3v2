// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a Modified
// BSD License that can be found in the LICENSE file.

package id3v2

//go:generate go run generate_ids.go

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
)

// This is an implementation of v2.4.0 of the ID3v2 tagging format,
// defined in: http://id3.org/id3v2.4.0-structure.

var (
	errIncompleteID3 = errors.New("id3: incomplete tag block")
	errInvalidID3    = errors.New("id3: invalid tag data")
)

const (
	flagUnsynchronisation = 1 << (7 - iota)
	flagExtendedHeader
	flagExperimental
	flagFooter
)

type FrameID uint32

func syncsafe(data []byte) (uint32, error) {
	_ = data[3]

	if data[0]&0x80 != 0 || data[1]&0x80 != 0 ||
		data[2]&0x80 != 0 || data[3]&0x80 != 0 {
		return 0, errInvalidID3
	}

	return uint32(data[0])<<21 | uint32(data[1])<<14 |
		uint32(data[2])<<7 | uint32(data[3]), nil
}

func id3Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	i := bytes.Index(data, []byte("ID3"))
	if i == -1 {
		if len(data) < 2 {
			return 0, nil, nil
		}

		return len(data) - 2, nil, nil
	}

	data = data[i:]
	if len(data) < 10 {
		if atEOF {
			return 0, nil, errIncompleteID3
		}

		return i, nil, nil
	}

	if data[3] == 0xff || data[4] == 0xff {
		return 0, nil, errInvalidID3
	}

	size, err := syncsafe(data[6:])
	if err != nil {
		return 0, nil, err
	}

	if len(data) < 10+int(size) {
		if atEOF {
			return 0, nil, errIncompleteID3
		}

		return i, nil, nil
	}

	if data[5]&flagFooter == flagFooter {
		size += 10
	}

	return i + 10 + int(size), data[:10+size], nil
}

var bufPool = &sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4<<10)
		return &buf
	},
}

func Scan(r io.Reader) (ID3Frames, error) {
	buf := bufPool.Get()
	defer bufPool.Put(buf)

	s := bufio.NewScanner(r)
	s.Buffer(*buf.(*[]byte), bufio.MaxScanTokenSize)
	s.Split(id3Split)

	var frames ID3Frames

scan:
	for s.Scan() {
		data := s.Bytes()

		header := data[:10]
		data = data[10:]

		if string(header[:3]) != "ID3" {
			panic("id3: bufio.Scanner failed")
		}

		switch header[3] {
		case 0x04:
		default:
			continue scan
		}

		flags := header[5]
		if flags&flagExtendedHeader == flagExtendedHeader {
			size, err := syncsafe(data)
			if err != nil {
				return nil, err
			}

			extendedHeader := data[:size]
			data = data[size:]

			_ = extendedHeader
		}

		if flags&flagFooter == flagFooter {
			footer := data[len(data)-10:]
			data = data[:len(data)-10]

			if string(footer[:3]) != "3DI" ||
				!bytes.Equal(header[3:], footer[3:]) {
				return nil, errInvalidID3
			}
		}

		for len(data) > 10 {
			_ = data[9]

			id := FrameID(data[0])<<24 | FrameID(data[1])<<16 |
				FrameID(data[2])<<8 | FrameID(data[3])

			size, err := syncsafe(data[4:])
			if err != nil {
				return nil, err
			}

			if len(data) < 10+int(size) {
				return nil, errInvalidID3
			}

			frames = append(frames, &ID3Frame{
				ID:    id,
				Flags: uint16(data[8])<<8 | uint16(data[9]),
				Data:  append([]byte(nil), data[10:10+size]...),
			})

			data = data[10+size:]
		}

		if flags&flagFooter == flagFooter && len(data) != 0 {
			return nil, errInvalidID3
		}

		for _, v := range data {
			if v != 0 {
				return nil, errInvalidID3
			}
		}
	}

	if s.Err() != nil {
		return nil, s.Err()
	}

	return frames, nil
}

type ID3Frames []*ID3Frame

func (frames ID3Frames) Lookup(id FrameID) *ID3Frame {
	for _, frame := range frames {
		if frame.ID == id {
			return frame
		}
	}

	return nil
}

type ID3Frame struct {
	ID    FrameID
	Flags uint16
	Data  []byte
}

func (f *ID3Frame) String() string {
	return fmt.Sprintf("&ID3Frame{ID: %s, Flags: %04x, Data: [%d]byte{...}}",
		f.ID.String(), f.Flags, len(f.Data))
}
